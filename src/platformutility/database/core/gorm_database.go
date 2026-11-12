package core

import (
	"sync"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
	"my.favorite/platformutility/config"
	"my.favorite/platformutility/database/dblogging"
	"my.favorite/platformutility/exception"
)

type GormDatabase struct {
	BaseDatabase
	gormDB *gorm.DB

	rwLock sync.RWMutex
}

func GetGormDatabase(storageCfg config.DataStorageConfig) IBaseDatabase {
	pool := GetDBPool(storageCfg)

	gormDb := &GormDatabase{
		BaseDatabase: BaseDatabase{
			accessorType: config.DatabaseGormAccessing, //通过GORMDB来访问数据
		},
		rwLock: sync.RWMutex{},
	}

	gormDb.SetPool(pool)
	return gormDb
}

func (g *GormDatabase) Open() error {
	g.rwLock.Lock()
	defer g.rwLock.Unlock()

	return g.openGormDatabase()
}

func (g *GormDatabase) Ping() error {
	if g.GetPool() == nil {
		return exception.DatabaseErrorEx("sqlite database connection pool is nil")
	}

	g.rwLock.RLock()
	defer g.rwLock.RUnlock()
	return g.GetPool().Ping()
}

func (g *GormDatabase) GetDB() *gorm.DB {
	g.rwLock.RLock()
	defer g.rwLock.RUnlock()

	return g.gormDB
}

func (g *GormDatabase) Close() {
	g.rwLock.Lock()
	defer g.rwLock.Unlock()

	if g.GetPool() == nil {
		return
	}

	g.GetPool().ClosePool()
	g.SetPool(nil)
}

// ==================== private functions & methods ============================
func (g *GormDatabase) openGormDatabase() error {
	if g.GetPool() == nil {
		return exception.DatabaseErrorEx("sqlite database connection pool is nil")
	}

	sqlDb, _ := g.GetPool().GetDatabase()
	if sqlDb == nil {
		err := g.OpenPoolWithRetry()
		if err != nil {
			return exception.DatabaseErrorEx(err.Error())
		}

		sqlDb, err = g.GetPool().GetDatabase()
	}

	var d gorm.Dialector
	switch g.GetPool().GetDbConfig().DBConfig.DBType {
	case config.ConstDbTypePostgresSQL:
		d = postgres.New(postgres.Config{Conn: sqlDb})
	case config.ConstDbTypeSqlite:
		d = sqlite.New(sqlite.Config{Conn: sqlDb})
	default:
		return exception.DatabaseErrorEx("database type can not be supported")
	}

	gormDb, err := gorm.Open(d, &gorm.Config{
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true, //禁止标明复数化
		},
		Logger: dblogging.GetDbLogger(g.GetPool().GetDbConfig().DBConfig.LogLevel), //logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return exception.DatabaseErrorEx(err.Error())
	}

	gormDb = gormDb.Session(&gorm.Session{
		PrepareStmt:            true,
		SkipDefaultTransaction: true, // 避免无畏的事务包裹
	})

	g.gormDB = gormDb
	return nil
}
