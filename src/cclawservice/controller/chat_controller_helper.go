package controller

import (
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
	"my.favorite/platformutility/webbase/middleware/controller"
)

var chatController = common.GetSingleton(func() *ChatController {
	return &ChatController{
		BaseController: controller.BaseController{
			Type:    basic.APIGroupRequire,
			Name:    "chatController",
			Service: nil,
		},
	}
}).Instance()

func GetChatController() *ChatController {
	return chatController
}
