from allgrpc import server
from milvus import Milvus
from File import Data

def store():
    f = Data()
    m = Milvus()

    path = "/home/chenyun/下载/train1.json"
    data,status = f.readFile(path)
    print(status)
    if data == "error/数据类型不匹配":
        print("err")
        return
    print("开始存储")
    m.storejson(data)
    print("存储完成")


store()