from allgrpc import server
from kafka.kafka import Kafka
from milvus import Milvus
from File import Data
import threading
from Log.log import logger
# from etcd import EtcdService

def store():
    f = Data()
    m = Milvus()

    path = "/home/chenyun/下载/train1.json"
    data = f.readFile(path)
    print("开始存储")
    m.storejson(data)
    print("存储完成")

def startkafka(kafka, topic, m, f):  # 将文件进行向量化存储
    kafka.consume_message(topic, m, f)

def RAG():
    f = Data()
    m = Milvus()
    server.server(m, f)  # 进行向量化查询

def threading_main():
    topic = "file_info_topic"
    m = Milvus()
    f = Data()
    k = Kafka()

    # 使用线程来运行 Kafka 消费者
    kafka_thread = threading.Thread(target=startkafka, args=(k, topic, m, f))
    kafka_thread.start()

    # 启动 RAG（你可以根据需求选择是否使用多线程来运行）
    rag_thread = threading.Thread(target=RAG, args=(m, f))
    rag_thread.start()

    # 等待线程结束
    kafka_thread.join()
    rag_thread.join()


if __name__ == "__main__":
    RAG()
