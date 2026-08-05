# 项目简介

## 1. done

- 生成SSL所需的cert/key文件命令：(open command line with Administrator)
  - 生成CA私钥
    openssl genrsa -out ca.key 2048 //生成CA私钥
  - 生成CA根证书
    openssl req -x509 -new -nodes -key ca.key -sha256 -days 3650 -out ca.crt -subj "/C=CN/ST=Beijing/L=Beijing/O=Dev/CN=Local CA"
  - 生成服务器私钥
    openssl genrsa -out server.key 2048
  - 生成CSR（不包含SAN）
    openssl req -new -key server.key -out server.csr  -subj "/C=CN/ST=Beijing/L=Beijing/O=Dev/CN=localhost"
    - 使用CA签署，同时添加SAN
      - 创建server.cnf文件，其文件内容：
        [req]
        default_bits = 2048
        prompt = no
        default_md = sha256
        distinguished_name = dn
        req_extensions = v3_req

        [dn]
        C = CN
        ST = Beijing
        L = Beijing
        O = Dev
        CN = localhost
      
        [v3_req]
        keyUsage = keyEncipherment, digitalSignature
        extendedKeyUsage = serverAuth
        subjectAltName = @alt_names
      
        [alt_names]
        DNS.1 = localhost
        DNS.2 = *.localhost
        IP.1 = 127.0.0.1
        IP.2 = ::1
      - 执行1：
        openssl req -new -key server.key -out server.csr -config server.cnf
      - 执行2：
        openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt -days 365 -sha256 -extensions v3_req -extfile server.cnf
  - 验证证书
    openssl x509 -in server.crt -text -noout | findstr "Subject Alternative Name"
  - 添加证书
    certutil -addstore "Root" ca.crt
    

- 基于sql.DB, gorm.DB进行数据库操作，实现分离
- 设置当前工作目录为 -- D:/MyIdea/CClaw/src
- 重新生成swagger文档
  - cd D:/MyIdea/CClaw/src/agentService
  - swag fmt ## 清空缓存
  - swag init -d .,./../platformutility,./../cclawservice --parseInternal --parseDependency --parseDependency ## 生成swagger文档

- Session相关
  - 通过SessionId(string) 唯一标识
  - 比如在chat.deepseek.com或类似的聊天AI聊天平台上点击“开启新对话”或新建聊条，就表示一个新的Session开始
  - 一个Session下会有聊天记录，通过聊天历史进行管理
  
- 聊天历史
  - 有很多message构成，每个message通过messageId进行标识
  - 每个message有方向，即用户的问题，与AI的回复

## 2. todo

- 基于sql.DB的数据库操作

  func (conn *CommonDatabase) Query(query string) (*sql.Rows, error) {
  if conn.GetPool() == nil {
  return nil, exception.DatabaseErrorEx("database connection need to be initialized")
  }

  queryString := strings.TrimSpace(strings.ToLower(query))
  if len(queryString) == 0 {
  return nil, exception.DatabaseErrorEx("query string cannot be empty")
  }

  //执行SQL
  var rows *sql.Rows
  err := conn.execute(func() error {
  var innerErr error
  db, _ := conn.GetPool().GetDatabase()
  rows, innerErr = db.Query(queryString)
  return innerErr
  })

  return rows, err
  }

  func (conn *CommonDatabase) QueryRow(query string) (*sql.Row, error) {
  if conn.GetPool() == nil {
  return nil, exception.DatabaseErrorEx("database connection need to be initialized")
  }

  queryString := strings.TrimSpace(strings.ToLower(query))
  if len(queryString) == 0 {
    return nil, exception.DatabaseErrorEx("query string cannot be empty")
  }

  //执行SQL语句
  var row *sql.Row
  err := conn.execute(func() error {
    db, _ := conn.GetPool().GetDatabase()
    row = db.QueryRow(queryString)
    return nil
  })

  return row, err
  }

  func (conn *CommonDatabase) Exec(query string) (sql.Result, error) {
  if conn.GetPool() == nil {
  return nil, exception.DatabaseErrorEx("database connection need to be initialized")
  }

  queryString := strings.TrimSpace(strings.ToLower(query))
  if len(queryString) == 0 {
    return nil, exception.DatabaseErrorEx("query string cannot be empty")
  }

  //执行SQL语句
  var result sql.Result
  err := conn.execute(func() error {
    var innerErr error
    db, _ := conn.GetPool().GetDatabase()
    result, innerErr = db.Exec(queryString)
    return innerErr
  })

  return result, err
  }

- 确定系统的工作目录，skill目录，session目录，memory目录等
  - skill的目录应该有系统的skill目录，个人的skill目录，项目的skill目录三种；
    - 个人的skill目录应该是：workspace/{person_}/{agent}/skill
    - 项目的skill：workspace/{project_name}/{agent}/skill
  - session 目录依赖于skill目录，即/workspace/skill/{session_name}/***
  - memory目录依赖于session:workspace/skill/{session_name}/{memory_date}
