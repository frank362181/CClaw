package main

import (
	"os"
	"path/filepath"

	"my.favorite/agentos"
	_ "my.favorite/cclaw/docs"
	"my.favorite/platformutility/common"
)

// @title           企业级AI助手操作系统
// @version         1.0.0
// @description     API服务入口

// @host      localhost:8443
// @BasePath  /api

// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name AUTHORIZATION
func main() {
	configFileName := filepath.Join(common.GetOSUtilityInstance().GetCurrentDir(), "cclaw.ini")
	if !agentos.InitApp(configFileName) {
		os.Exit(-1)
	}

	agentos.SetupGlobalFilter()

	// 添加 health controller
	agentos.SetupHealthController()
	agentos.SetupLoggingController()

	// 1. setup server
	agentos.SetupServer()

	// 2. start the server
	agentos.Startup()

	// 3. wait the exit signal
	agentos.Shutdown()
}
