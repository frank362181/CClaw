package core

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"my.favorite/platformutility/config"
	"my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
)

type CommonDatabase struct {
	BaseDatabase

	connected bool
	rwLock    sync.RWMutex
}

/*
* 通用数据库操作类
 */

func GetCommonDatabase(config *config.ConfigurationHelper) IBaseDatabase {
	pool := GetDBPool(config) //根据类型创建
	if pool == nil {
		return nil
	}

	db := &CommonDatabase{
		connected: false, //如果非空,则已经建立了连接池
		rwLock:    sync.RWMutex{},

		BaseDatabase: BaseDatabase{
			accessorType: "sqldb", //通过SQLDB来访问数据库
		},
	}
	db.SetPool(pool)
	return db
}

func (conn *CommonDatabase) Open() error {
	conn.rwLock.Lock()
	defer conn.rwLock.Unlock()

	return conn.openDatabase()
}

func (conn *CommonDatabase) Ping() error {
	if conn.GetPool() == nil {
		return exception.DatabaseErrorEx("database connection need to be initialized")
	}

	return conn.execute(
		func() error {
			return conn.GetPool().Ping()
		},
	)
}

func (conn *CommonDatabase) GetDB() *sql.DB {
	conn.rwLock.RLock()
	defer conn.rwLock.RUnlock()

	db, _ := conn.GetPool().GetDatabase()
	return db
}

func (conn *CommonDatabase) Close() {
	conn.rwLock.Lock()
	defer conn.rwLock.Unlock()

	if conn.GetPool() == nil {
		return
	}

	conn.GetPool().ClosePool()
	conn.SetPool(nil)

	conn.connected = false
}

// ============================= private functions & methods  ========================================

func (conn *CommonDatabase) openDatabase() error {
	if conn.GetPool() == nil {
		return exception.DatabaseErrorEx("database connection pool is nil")
	}

	if conn.connected {
		_, err := conn.GetPool().GetDatabase()
		if err != nil {
			return err
		}
	}

	if conn.GetPool().IsOpened() {
		_, err := conn.GetPool().GetDatabase()
		if err != nil {
			return err
		}
	}

	//open pool
	err := conn.GetPool().OpenPool()
	if err != nil {
		return err
	}

	conn.connected = true
	_, err = conn.GetPool().GetDatabase()
	return err
}

func (conn *CommonDatabase) execute(operation func() error) error {
	conn.rwLock.Lock()

	// 1. 如果链接已经断开,尝试重新链接
	if !conn.connected {
		err := conn.openDatabase()
		if err != nil {
			conn.rwLock.Unlock()
			logging.LogErrorF(fmt.Sprintf("failed to reconnect the database : %s", err.Error()))
			return err
		}
	}

	conn.rwLock.Unlock() //释放锁方便数据库操作

	// 2. 数据库操作是支持多线程的
	err := operation() //返回的错误有可能是SQL语句错误
	if err != nil {
		conn.rwLock.Lock()
		defer conn.rwLock.Unlock()

		//此时需要判断是否是链接问题
		if errors.Is(err, exception.DatabaseErr) {
			if conn.GetPool() != nil {
				conn.GetPool().ClosePool()
			}
			conn.connected = false
		}
	}
	return err
}
