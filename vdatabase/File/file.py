
import re
import json

class Data:
    def __init__(self):
        pass

    def readFile(self,filepath):
        datatype = self.detect_file_type(filepath)
        print("获取数据 " + datatype)
        if datatype == 'Unknown':
            return "文件错误"
        try:
            print("尝试解析")
            with open(filepath,'r',encoding="utf-8") as file:
                print("打开文件")
                match datatype:
                    case 'JSON':
                        try:
                            print("开始解析")
                            data = json.load(file)
                            print("解析成功")
                            return data,'JSON'
                        except FileNotFoundError:
                            print(f"错误: JSON 文件 '{filepath}' 未找到.")
                            return
                        except json.JSONDecodeError:
                            print(f"错误: JSON 文件 '{filepath}' 格式不正确.")
                            return
                    case 'PDF':
                        pass
                    case 'Word':
                        pass
                    case 'Markdown':
                        pass
                    case _:
                        return "error/数据类型不匹配"
        except Exception as e:
            print("失败")
            return "error" "error"

    # 读取文件，返回文件类型
    def detect_file_type(self, file_path):
        try:
            with open(file_path, 'rb') as f:
                header = f.read(8)  # 读取前 8 个字节
                # 判断 PDF 文件
                if header.startswith(b'%PDF'):
                    return 'PDF'
                # 判断 Word 文件（ZIP 格式）
                if header.startswith(b'\x50\x4B\x03\x04'):
                    return  'Word'


            # 尝试解析 JSON 文件
            try:
                with open(file_path, 'r', encoding='utf-8') as f:
                    json.load(f)  # 尝试解析 JSON
                return 'JSON'
            except json.JSONDecodeError:
                pass

            # 尝试解析 Markdown 文件
            try:
                with open(file_path, 'r', encoding='utf-8') as f:
                    content = f.read(1024)  # 读取前 1024 字节
                    # 判断是否为 Markdown 格式
                    if re.search(r'^#|[-*]|!\[.*\]\(.*\)|\[.*\]\(.*\)', content):
                        return 'Markdown'
            except Exception:
                pass

            # 如果无法确定类型，则返回未知文件类型
            return 'Unknown'

        except Exception as e:
            return 'Error'