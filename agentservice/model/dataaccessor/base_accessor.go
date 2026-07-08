package dataaccessor

import (
	"my.favorite/agentservice/model/dataobject"
	"my.favorite/platformutility/database/core"
)

type BaseAccessor struct {
	db core.IBaseDatabase
}

type IAccessor interface {
	GetDatabase() core.IBaseDatabase
	GetUserByNameAndPassword(username, userpass string) (*dataobject.T_Account, error)
}

func (accessor *BaseAccessor) GetDatabase() core.IBaseDatabase {
	return accessor.db
}

type GormAccessor struct {
	BaseAccessor
}

type SQLdbAccessor struct {
	BaseAccessor
}

func GetGormAccessor(db core.IBaseDatabase) IAccessor {
	gormAccessor := &GormAccessor{
		BaseAccessor: BaseAccessor{
			db: db,
		},
	}

	return gormAccessor
}

func GetSQLdbAccessor(db core.IBaseDatabase) IAccessor {
	//hsqldbAccessor := &SQLdbAccessor{
	//	BaseAccessor: BaseAccessor{
	//		db: db,
	//	},
	//}
	//
	//return hsqldbAccessor
	return nil
}
