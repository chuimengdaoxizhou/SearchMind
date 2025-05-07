import grpc
from allgrpc.allproto import protos_pb2,protos_pb2_grpc


def getdata(prompt):
    with grpc.insecure_channel("localhost:50051") as channel:
        stub = protos_pb2_grpc.DataManagementStub(channel)
        rsp: protos_pb2.Response = stub.getDatabyPrompt(protos_pb2.Request(prompt=prompt))
        print(rsp.answer)

def updata(filepath):
    with grpc.insecure_channel("localhost:50051") as channel:
        stub = protos_pb2_grpc.DataManagementStub(channel)
        rsp: protos_pb2.Response = stub.updatabypath(protos_pb2.Request(prompt=filepath))
        print(rsp.answer)


