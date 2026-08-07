package service

import (
	"strings"

	"my.favorite/cclawservice/model/dataaccessor"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/database/core"
	memoryDbCommon "my.favorite/platformutility/memory_db/common"
	"my.favorite/platformutility/webbase/middleware/service"
)

var userService = common.GetSingleton(func() *LoginService {
	return &LoginService{
		BaseService: service.BaseService{
			Name:        "userService",
			Description: "service about the operation over user",
		},
	}
}).Instance()

func GetUserService(memoryDb memoryDbCommon.IMemoryDB, db core.IBaseDatabase) *LoginService {
	//设置内存访问
	userService.memoryDb = memoryDb

	//设置数据库访问的类
	if strings.EqualFold(db.GetAccessorType(), "sqldb") {
		userService.dbAccessor = dataaccessor.GetSQLdbAccessor(db)
		return userService
	} else if strings.EqualFold(db.GetAccessorType(), "gormdb") {
		userService.dbAccessor = dataaccessor.GetGormAccessor(db)
		return userService
	}

	return nil
}
