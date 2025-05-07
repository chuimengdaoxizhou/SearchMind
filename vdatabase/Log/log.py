import logging
from logging.handlers import RotatingFileHandler

# 创建日志记录器
logger = logging.getLogger()

# 设置日志级别
logger.setLevel(logging.DEBUG)

# 创建日志文件处理器，并设置日志轮换
file_handler = RotatingFileHandler('app.log', maxBytes=5 * 1024 * 1024, backupCount=3)
file_handler.setLevel(logging.DEBUG)

# 创建日志格式器
formatter = logging.Formatter('%(asctime)s - %(levelname)s - %(message)s')
file_handler.setFormatter(formatter)

# 将文件处理器添加到日志记录器
logger.addHandler(file_handler)

