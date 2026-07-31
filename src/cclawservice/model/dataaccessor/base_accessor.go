package dataaccessor

import (
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/database/core"
)

type BaseAccessor struct {
	db core.IBaseDatabase
}

type IAccessor interface {
	GetDatabase() core.IBaseDatabase
	GetUserByNameAndPassword(username, userpass string) (*dataobject.T_Account, error)
	GetUserByAccount(username string) (*dataobject.T_Account, error)
}

func (accessor *BaseAccessor) GetDatabase() core.IBaseDatabase {
	return accessor.db
}

type GormAccessor struct {
	BaseAccessor
}

var gormAccessor = pCommon.GetSingleton(func() *GormAccessor {
	return &GormAccessor{
		BaseAccessor: BaseAccessor{
			db: nil,
		},
	}
}).Instance()

func GetGormAccessor(db core.IBaseDatabase) IAccessor {
	gormAccessor.db = db
	return gormAccessor
}

type SQLdbAccessor struct {
	BaseAccessor
}

var sqlDbAccessor = pCommon.GetSingleton(func() *SQLdbAccessor {
	return &SQLdbAccessor{
		BaseAccessor: BaseAccessor{
			db: nil,
		},
	}
}).Instance()

func GetSQLdbAccessor(db core.IBaseDatabase) IAccessor {
	// sqlDbAccessor.db = db
	// return sqlDbAccessor
	return nil
}
