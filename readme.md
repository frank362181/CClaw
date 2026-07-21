### done
- 生成SSL所需的cert/key文件命令：

    openssl req -x509 -newkey rsa:4096 -keyout server.key -out server.crt -days 365 -nodes -subj "/CN=localhost"

- 基于sql.DB, gorm.DB进行数据库操作，实现分离


### todo
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


