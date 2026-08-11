package dataaccessor

import (
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/database/core"
)

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
