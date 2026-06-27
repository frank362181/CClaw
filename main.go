package main

import (
	"os"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/config"
	"my.favorite/platformutility/webbase"
	"my.favorite/platformutility/webbase/middleware/filter"
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
	helper, err := config.GetConfigurationHelper(configFileName)
	if err != nil {
		os.Exit(-1)
	}
	err = helper.LoadConfiguration()
	if err != nil {
		os.Exit(-1)
	}

	router := webbase.GetBaseRouter()
	if router == nil {
		return
	}

	// 1. setup server
	router.SetupServer(helper)

	//注册global filter,此时将确保系统默认的FILTER全局仅有一个实例
	recoveryFilter := filter.GetRecoveryFilter()
	loggingFilter := filter.GetLoggingFilter()
	reqPreprocessFilter := filter.GetRequestPreprocessFilter()
	addressFilter := filter.GetAddressFilter()
	securityFilter := filter.GetSecurityFilter()
	router.SetupFilter(recoveryFilter, loggingFilter, reqPreprocessFilter, addressFilter, securityFilter)

	// 添加 interceptor

	// 添加 controller

	// 2. start the server
	sslEnabled, _ := helper.GetBoolValueDefault("server", "ssl", false)
	router.Startup(sslEnabled)

	// 3. wait the exit signal
	router.Shutdown()
}
