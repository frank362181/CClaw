package contnroller

import (
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
	"my.favorite/platformutility/webbase/middleware/controller"
	"my.favorite/platformutility/webbase/middleware/service"
)

var loginController = common.GetSingleton(func() *LoginController {
	return &LoginController{
		BaseController: controller.BaseController{
			Type:    basic.APIGroupRequire,
			Name:    "loggingController",
			Service: nil,
		},
	}
}).Instance()

// GetLoginController 注入一个服务实现类
func GetLoginController(service service.IService) *LoginController {
	loginController.Service = service

	return loginController
}
