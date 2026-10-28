package dataaccessor

import (
	"my.favorite/platformutility/database/core"
)

type BaseAccessor struct {
	db core.IBaseDatabase
}

func (accessor *BaseAccessor) GetDatabase() core.IBaseDatabase {
	return accessor.db
}
