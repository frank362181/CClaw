package agentosservice

import (
	"strings"

	"my.favorite/agentservice/model/dataaccessor"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/database/core"
	"my.favorite/platformutility/webbase/middleware/service"
)

var userService = common.GetSingleton(func() *UserService {
	return &UserService{
		BaseService: service.BaseService{
			Name:        "userService",
			Description: "service about the operation over user",
		},
	}
}).Instance()

func GetUserService(db core.IBaseDatabase) *UserService {
	if strings.EqualFold(db.GetAccessorType(), "sqldb") {
		userService.dbAccessor = dataaccessor.GetSQLdbAccessor(db)
		return userService
	} else if strings.EqualFold(db.GetAccessorType(), "gormdb") {
		userService.dbAccessor = dataaccessor.GetGormAccessor(db)
		return userService
	}

	return nil
}
