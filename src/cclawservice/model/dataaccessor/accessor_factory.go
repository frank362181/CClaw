package dataaccessor

import (
	"strings"

	"my.favorite/platformutility/database/core"
	"my.favorite/platformutility/exception"
)

func GetAccessor(db core.IBaseDatabase) (IAccessor, error) {
	var accessor IAccessor
	switch {
	case strings.EqualFold(db.GetAccessorType(), core.DatabaseCommonAccessing):
		accessor = GetSQLdbAccessor(db)
	case strings.EqualFold(db.GetAccessorType(), core.DatabaseGormAccessing):
		accessor = GetGormAccessor(db)
	default:
		return nil, exception.UnsupportedExceptionErrorEx2("login service: database type[%s] is unsupported", db.GetAccessorType())
	}

	if accessor == nil {
		return nil, exception.UnsupportedExceptionErrorEx2("login service: database type[%s] is unsupported", db.GetAccessorType())
	}

	return accessor, nil
}
