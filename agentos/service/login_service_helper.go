package service

import (
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/service"
)

var loginService = common.GetSingleton(func() *LoginService {
	return &LoginService{
		BaseService: service.BaseService{
			Name:        "LoginService",
			Description: "login service",
		},
	}
}).Instance()

func GetLoginService() *LoginService {
	return loginService
}
