package pgdatabase

import (
	"sync"

	"my.favorite/platformutility/database"
)

type PGConnection struct {
	database.BaseDBConnection

	connected bool
	rwLock    sync.RWMutex
}

/*
* GetPGConnection 工厂方法,根据数据库类型不同获取
 */

func GetPGConnection(dbPool database.IBaseDBPool) database.IBaseDBConnection {
	conn := &PGConnection{
		connected: dbPool != nil, //如果非空,则已经建立了连接池
		rwLock:    sync.RWMutex{},

		BaseDBConnection: database.BaseDBConnection{},
	}

	conn.SetPool(dbPool)
	return conn
}

func (conn *PGConnection) Ping() error {
	return conn.GetPool().Ping()
}

func (conn *PGConnection) Query(query string, args ...interface{}) error {
	return nil
}

func (conn *PGConnection) QueryRow(query string, args ...interface{}) error {
	return nil
}

func (conn *PGConnection) Exec(query string, args ...interface{}) error {
	return nil
}

func (conn *PGConnection) Close() {
	conn.GetPool().ClosePool()
}
