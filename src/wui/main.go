package main

import (
	"os"

	_ "my.favorite/cclaw/docs"
	"my.favorite/cclawservice"
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
	////tui_engine.GetLogManager().Start()
	//
	version_string := VersionString
	logging.LogInfof(version_string)

	configFileName := GetConfigFileName()
	if !cclawservice.InitApp(configFileName) {
		os.Exit(-1)
	}

	cclawservice.SetupGlobalFilter()

	// 添加 health controller
	cclawservice.SetupHealthController()
	cclawservice.SetupLoggingController()

	// 1. setup server
	cclawservice.SetupServer()

	// 2. start the server
	cclawservice.Startup()

	// 3. wait the exit signal
	cclawservice.Shutdown()

	//// 结束TUI
	//tui_engine.GetLogManager().Stop()
}
