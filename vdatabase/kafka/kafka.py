import json
import confluent_kafka
from confluent_kafka import KafkaException
from confluent_kafka.admin import AdminClient
from confluent_kafka.cimpl import NewTopic, Consumer, KafkaError
from minio import Minio
from minio.error import S3Error
import os

from Log.log import logger

class Kafka:
    def __init__(self):
        logger.debug("Kafka init")
        print()
        # 配置消费者
        conf = {
            'bootstrap.servers': 'localhost:9092',  # Kafka 集群地址
            'group.id': 'file_info_group',           # 消费者组
            'auto.offset.reset': 'earliest'          # 从最早的消息开始消费
        }

        self.consumer = Consumer(conf)
    def consume_message(self, topic, Milvus, File):
        logger.debug("Kafka consume_message start...")
        self.create_topic_if_not_exists(topic)  # 调用创建主题方法
        # 订阅主题
        self.consumer.subscribe([topic])
        self.consumer.assign([confluent_kafka.TopicPartition(topic, 0)])  # 指定分区为0

        try:
            while True:
                # 拉取消息
                msg = self.consumer.poll(1.0)  # 超时为 1 秒

                if msg is None:
                    # 没有消息，继续等待
                    continue
                print("获取到信息")
                if msg.error():
                    # 如果有错误，处理错误
                    error = msg.error()
                    if error.code() == KafkaError._PARTITION_EOF:
                        # 到达分区末尾，不做处理，继续拉取
                        continue
                    else:
                        raise KafkaException(error)

                # 打印消息内容
                data = json.loads(msg.value().decode('utf-8'))
                filename = data['fileName']
                userid = data['userid']
                # 从 MinIO 下载文件
                FilePath = read_file_from_minio(filename, userid)
                # 读取文件
                filedata, status = File.readFile(FilePath)
                if filedata == "error/数据类型不匹配":
                    print("error/数据类型不匹配")
                    logger.error(userid + "上传的" + FilePath + "文件类型错误")
                    return
                Milvus.storejson(filedata)
                logger.fatal(userid + "上传的" + FilePath + "存储成功")

        finally:
            # 关闭消费者
            logger.debug("Kafka consume_message end...")
            self.consumer.close()

    def create_topic_if_not_exists(self, topic_name):  # 这里加上了 self 参数
        conf = {
            'bootstrap.servers': 'localhost:9092',  # Kafka 集群地址
        }

        admin_client = AdminClient(conf)

        # 检查当前集群中是否存在该主题
        try:
            # 获取所有现有的主题
            existing_topics = admin_client.list_topics().topics

            if topic_name in existing_topics:
                print(f"主题 {topic_name} 已经存在，无需创建。")
            else:
                # 创建一个新主题，指定分区数和副本因子
                topic = NewTopic(topic_name, num_partitions=1, replication_factor=1)
                fs = admin_client.create_topics([topic])

                # 等待创建结果
                fs[topic_name].result()  # 如果成功，会返回 None
                print(f"主题 {topic_name} 创建成功！")

        except KafkaException as e:
            print(f"检查或创建主题 {topic_name} 时出错: {e}")




def read_file_from_minio(filename, bucket_name):
    # 创建 MinIO 客户端
    client = Minio(
        "localhost:9000",  # MinIO 服务的地址
        access_key="my_access_key",  # MinIO Access Key
        secret_key="my_secret_key",  # MinIO Secret Key
        secure=False  # 如果使用 http 协议而不是 https
    )
    download_dir = "./temp/"
    try:
        # 获取文件的对象
        file_obj = client.get_object(bucket_name, filename)

        # 读取文件内容
        file_content = file_obj.read()

        # 确保下载目录存在，如果不存在则创建
        if not os.path.exists(download_dir):
            os.makedirs(download_dir)

        # 定义本地文件路径
        local_file_path = os.path.join(download_dir, filename)

        # 将文件内容写入本地文件
        with open(local_file_path, "wb") as f:
            f.write(file_content)

        print(f"File {filename} has been downloaded to {local_file_path}")
        return local_file_path
    except S3Error as e:
        print(f"Error occurred: {e}")

