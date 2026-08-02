# 项目简介

## 1. done

- 生成SSL所需的cert/key文件命令：

    openssl req -x509 -newkey rsa:4096 -keyout server.key -out server.crt -days 365 -nodes -subj "/CN=localhost"

- 基于sql.DB, gorm.DB进行数据库操作，实现分离
- 设置当前工作目录为 -- D:/MyIdea/CClaw/src
- 重新生成swagger文档
  - cd D:/MyIdea/CClaw/src/wui
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
