/*
* 这里必须要加上,否则sql.Open时会报forget import
**/

package core

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
)

type SqliteDBPool struct {
	BaseDBPool

	db *sql.DB
}

func (pool *SqliteDBPool) IsOpened() bool {
	return pool.db != nil
}

func (pool *SqliteDBPool) OpenPool() error {
	pool.ClosePool()

	//读取配置文件
	dbPath, err1 := common.ResolveFile(common.GetCurrentDir(), pool.DBConfig.DatabaseName) //包含路径
	if err1 != nil {
		return err1
	}

	//设置链接字符串
	dsn := fmt.Sprintf("file:%s?cache=shared&_journal_mode=WAL&_busy_timeout=%d", dbPath, pool.DBConfig.MaxRetries*1000)
	var err error
	pool.db, err = sql.Open("sqlite", dsn)
	if err != nil {
		return exception.DatabaseErrorEx(err.Error())
	}

	//SQLite设置,这里的配置一定要为 1，因为底层是文件
	pool.db.SetMaxOpenConns(1)
	pool.db.SetMaxIdleConns(1)
	pool.db.SetConnMaxLifetime(time.Hour)

	//测试链接
	if err = pool.Ping(); err != nil {
		return exception.DatabaseErrorEx(err.Error())
	}
	logging.LogInfo("Sqlite3 Database established: ", dbPath)

	return nil
}

func (pool *SqliteDBPool) GetDatabase() (*sql.DB, error) {
	if pool.IsOpened() {
		if pool.db != nil {
			return pool.db, nil
		}
	}

	return nil, exception.DatabaseErrorEx("sqlite database is not open")
}

func (pool *SqliteDBPool) Ping() error {
	if pool.IsOpened() {
		err := pool.db.Ping()
		if err != nil {
			return exception.DatabaseErrorEx(err.Error())
		}

		return nil
	}

	return exception.DatabaseErrorEx("sqlite database is not open")
}

func (pool *SqliteDBPool) ClosePool() {
	if pool.IsOpened() {
		_ = pool.db.Close()
		pool.db = nil
	}
}
