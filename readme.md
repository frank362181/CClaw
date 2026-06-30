### done
- 生成SSL所需的cert/key文件命令：

openssl req -x509 -newkey rsa:4096 -keyout server.key -out server.crt -days 365 -nodes -subj "/CN=localhost"


### todo
- PostgreSQL数据库连接池设计完成，connection是对外暴露的操作数据库的核心类，设计未完成
- 是否存在类似java中Mybatis mapper的设计机制 ？
- OpenClaw 代码阅读开了个头，已经知道入口函数，以及核心的函数