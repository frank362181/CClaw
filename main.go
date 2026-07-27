package main

import (
	"os"
	"path/filepath"

	agentservice "my.favorite/agentservice"
	_ "my.favorite/cclaw/docs"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"
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
	// 开启运行界面
	//tui_engine.GetLogManager().Start()

	version_string := GetVersion()
	logging.LogInfo(version_string)

	configFileName := filepath.Join(common.GetCurrentDir(), "cclaw.ini")
	if !agentservice.InitApp(configFileName) {
		os.Exit(-1)
	}

	agentservice.SetupGlobalFilter()

	// 添加 health controller
	agentservice.SetupHealthController()
	agentservice.SetupLoggingController()

	// 1. setup server
	agentservice.SetupServer()

	// 2. start the server
	agentservice.Startup()

	// 3. wait the exit signal
	agentservice.Shutdown()

	// 结束TUI
	//tui_engine.GetLogManager().Stop()
}
