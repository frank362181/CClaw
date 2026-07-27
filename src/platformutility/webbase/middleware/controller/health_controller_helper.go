package controller

import (
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

var healthController = common.GetSingleton(func() *HealthController {
	return &HealthController{
		BaseController: BaseController{
			Type: basic.APIGroupCommon,
			Name: "HealthController",
		},
	}
}).Instance()

func GetHealthController() *HealthController {
	return healthController
}
