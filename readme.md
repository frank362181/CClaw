### done
- 生成SSL所需的cert/key文件命令：

openssl req -x509 -newkey rsa:4096 -keyout server.key -out server.crt -days 365 -nodes -subj "/CN=localhost"


### todo
- PostgreSQL数据库连接池设计完成，connection是对外暴露的操作数据库的核心类，设计未完成
- 如果是Mysql数据库，池化部分依然可以这样设计，对外暴露用sql.DB进行包装
- 是否存在类似java中Mybatis mapper的设计机制 ？ -- 可以使用sql.DB来进行桥接 ，进而实现标准化




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