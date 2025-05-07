import grpc
from concurrent import futures
from Log.log import logger
from allgrpc.allproto import protos_pb2,protos_pb2_grpc

class Getdata(protos_pb2_grpc.DataManagementServicer):
    def __init__(self, milvus, file):
        self.milvus = milvus
        self.file = file
    def getDatabyPrompt(self, request, context):
        try:
            self.milvus.checkconnection()
            prompt = request.prompt
            print("prompt: " + prompt)
            answer = self.milvus.getdata(prompt)
            if answer is None:
                return protos_pb2.Response(answer="未找到匹配的结果")
            print("result: " + answer)
            return protos_pb2.Response(answer=answer)
        except Exception as e:
            print(f"Error handling request: {e}")
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(f"Internal error: {e}")
            return protos_pb2.Response(answer="Internal error")

    def updatabypath(self, request, context):
        print(f"接收到数据文件路径：{request.prompt}")
        filepath = request.prompt
        data,filetype = self.file.readFile(filepath)
        match filetype:
            case 'JSON':
                flag = self.milvus.storejson(data)
                if flag:
                    return protos_pb2.Response(answer="存储成功")
                else:
                    return protos_pb2.Response(answer="存储失败")

            case _:
                return protos_pb2.Response(answer=f"存储失败，可能文件格式不正确" )



def server(milvus,file):
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    protos_pb2_grpc.add_DataManagementServicer_to_server(Getdata(milvus,file), server)
    server.add_insecure_port('[::]:50051')
    server.start()
    print("服务器启动")
    try:
        server.wait_for_termination()
    except KeyboardInterrupt:
        print("服务器停止")
        server.stop(0)

# def etcdserver(milvus,file, etcdservice):
#     try:
#         # 创建 grpc 服务器
#         grpc_server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
#         protos_pb2_grpc.add_DataManagementServicer_to_server(Getdata(milvus,file), grpc_server)
#         host = "localhost"
#         port = "50051"
#         grpc_server.add_insecure_port(f"{host}:{port}")
#
#         # 注册服务到 etcdservice
#         service_info = {
#             "host": host,
#             "port": port,
#             "name": "grpc-data-service"
#         }
#         lease = etcdservice.register_service("grpc-data-service", service_info)
#
#         # 启动 grpc 服务器
#         grpc_server.start()
#         logger.info(f"grpc server started at {host}:{port}")
#
#         try:
#             grpc_server.wait_for_termination()
#         except KeyboardInterrupt:
#             logger.info("Shutdown down grpc server ...")
#             grpc_server.stop(0)
#             # 注销服务
#             etcdservice.deregister_service("grpc-data-service",host,port)
#
#     except Exception as e:
#         logger.error(f"Failed to start grpc server: {e}")
#         raise
#
