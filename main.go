package main

import (
	"fmt"

	_ "project/docs"

	"my.favorite/platformutility/common"
)

// @title           企业级AI助手操作系统
// @version         1.0
// @description     系统应用入口

// @host      localhost:8443
// @BasePath  /api/v1

// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name AUTHORIZATION
func main() {
	configFileName := common.GetOSUtilityInstance().GetCurrentDir() + "/cclaw.ini"

	fmt.Println(configFileName)

}
