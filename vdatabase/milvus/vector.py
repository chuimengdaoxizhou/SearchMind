import threading
from pymilvus import connections, Collection, FieldSchema, CollectionSchema, DataType, utility
from langchain_huggingface import HuggingFaceEmbeddings
from tqdm import trange
import time
import threading
import os


class Milvus:
    def __init__(self):
        print("初始化Milvus")
        self.lock = threading.Lock()
        self.checkconnection()
        self.collection_name = "qa_collection"

        if utility.has_collection(self.collection_name):
            self.collection = Collection(name=self.collection_name)
            print("初始化，将数据加载到内存")
            self.collection.load()
        else:
            self.collection = None
            print(f"没有找到名为 '{self.collection_name}' 的集合，数据没有加载。")

        self.model_path = "/home/chenyun/program/python/vdatabase/models/sentence_model"
        model_name = "sentence-transformers/all-mpnet-base-v2"
        print("开始加载模型")

        # 如果目录为空或不存在，则使用缓存目录下载
        if not os.path.exists(self.model_path) or not os.listdir(self.model_path):
            os.makedirs(self.model_path, exist_ok=True)
            print(f"模型目录为空或不存在，将从 Hugging Face 下载模型到: {self.model_path}")
        else:
            print(f"模型目录存在，将尝试从本地加载模型: {self.model_path}")

        self.embeddings = HuggingFaceEmbeddings(
            model_name=model_name,
            cache_folder=self.model_path  # langchain 会自动下载或读取
        )
        print("加载模型完成")
        print("初始化完成")
    def checkconnection(self):
        try:
            print(f"{time.time()}: 检查连接状态...")
            if not connections.has_connection("default"):
                print("尝试连接到 Milvus...")
                with self.lock:
                    if not connections.has_connection("default"):
                        print(f"{time.time()}: 进入锁，准备连接...")
                        connections.connect(
                            alias="default",
                            host="localhost",
                            port="19530",
                            timeout=5
                        )
                        print(f"{time.time()}: 连接成功完成！")
            else:
                print(f"{time.time()}: 已经连接到 Milvus。")
            time.sleep(2)  # 等待连接完全建立
        except Exception as e:
            print(f"连接失败: {e}")

    def close(self):
        print(f"{time.time()}: 开始关闭milvus")
        try:
            print(f"{time.time()}: 尝试释放内存和断开连接")
            if self.collection is not None:
                print(f"{time.time()}: 释放集合...")
                self.collection.release()
                print(f"{time.time()}: 集合已释放")
                self.collection = None  # 显式置空，避免重复释放
            if connections.has_connection("default"):
                with self.lock:
                    print(f"{time.time()}: 断开连接...")
                    connections.disconnect("default")
                    print(f"{time.time()}: 已断开连接")
        except Exception as e:
            print(f"{time.time()}: 断开连接失败: {e}")
        finally:
            print(f"{time.time()}: close 方法完成")

    def getcollections(self):
        with self.lock:
            collections = utility.list_collections()
            return collections

    def deletecollections(self):
        with self.lock:
            collections = utility.list_collections()
            for collection in collections:
                utility.drop_collection(collection_name=collection)

    def getdata(self, prompt):
        if not utility.has_collection(self.collection_name):
            self.collection = Collection(name=self.collection_name)
            print("将数据加载到内存")
            self.collection.load()

        print(f"{time.time()}: 开始查找: {prompt}")
        print(f"{time.time()}: 检查是否连接")
        self.checkconnection()
        try:
            print(f"{time.time()}: 开始向量化数据")
            query_embedding = self.embeddings.embed_query(prompt)
            print(f"{time.time()}: 开始搜索")
            search_params = {"metric_type": "L2"}  # FLAT 索引不需要 nprobe 参数
            with self.lock:
                results = self.collection.search(
                    data=[query_embedding],
                    anns_field="embedding",
                    param=search_params,
                    limit=1,
                    output_fields=["instruction", "output"]
                )
            print(f"{time.time()}: 搜索完成")

            # 检查结果是否有效
            if not results or not results[0]:
                print(f"{time.time()}: 无搜索结果")
                return

            # 获取最近匹配的距离和内容
            distance = results[0][0].distance
            matched_instruction = results[0][0].entity.get('instruction')
            print(f"{time.time()}: 最近匹配距离: {distance}, 匹配的问题: {matched_instruction}")

            # 设置更严格的距离阈值，例如 0.5（可以根据实际情况调整）
            if distance > 0.5:  # 如果距离大于 0.5，认为不匹配
                print(f"{time.time()}: 距离 {distance} 超过阈值 0.5，未找到匹配结果")
                return

            # 返回匹配的输出
            return results[0][0].entity.get('output')
        except Exception as e:
            print(f"查询失败: {e}")
            return
    def storejson(self, json_data):
        with self.lock:
            try:
                print("检查是否连接")
                self.checkconnection()
                instructions = [item["instruction"] for item in json_data]
                output = [item["output"] for item in json_data]
                print("开始向量化数据")
                embedded_instructions = self.embeddings.embed_documents(instructions)

                collection_name = "qa_collection"
                if not utility.has_collection(collection_name):
                    fields = [
                        FieldSchema(name="id", dtype=DataType.INT64, is_primary=True, auto_id=True),
                        FieldSchema(name="instruction", dtype=DataType.VARCHAR, max_length=512),
                        FieldSchema(name="output", dtype=DataType.VARCHAR, max_length=4096),
                        FieldSchema(name="embedding", dtype=DataType.FLOAT_VECTOR, dim=768)
                    ]
                    schema = CollectionSchema(fields=fields, description="QA collection")
                    self.collection = Collection(name=collection_name, schema=schema)
                    print(f"创建了新的 collection: {collection_name}")
                else:
                    self.collection = Collection(name=collection_name)
                    print(f"使用现有的 collection: {collection_name}")

                print("开始插入数据")
                chunk_size = 10
                for i in trange(0, len(instructions), chunk_size):
                    self.collection.insert([
                        instructions[i:i + chunk_size],
                        output[i:i + chunk_size],
                        embedded_instructions[i:i + chunk_size]
                    ])

                self.collection.create_index(
                    field_name="embedding",
                    index_params={"metric_type": "L2", "index_type": "FLAT"}
                )
                self.collection = Collection(name=self.collection_name)
                print("将数据重新加载到内存")
                self.collection.load()
                print("Data successfully stored in Milvus.")
                return True
            except Exception as e:
                print(f"存储失败: {e}")
                return False
    def __del__(self):
        print(f"{time.time()}: 关闭Milvus")
        self.close()
        print(f"{time.time()}: 关闭完成")

