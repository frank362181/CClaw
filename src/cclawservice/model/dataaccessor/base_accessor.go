package dataaccessor

import (
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/platformutility/database/core"
)

type BaseAccessor struct {
	db core.IBaseDatabase
}

type IAccessor interface {
	GetDatabase() core.IBaseDatabase
	GetUserByNameAndPassword(username, userpass string) (*dataobject.TAccount, error)
	GetUserByAccount(username string) (*dataobject.TAccount, error)
}

func (accessor *BaseAccessor) GetDatabase() core.IBaseDatabase {
	return accessor.db
}
