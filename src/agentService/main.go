package main

import (
	_ "my.favorite/cclaw/docs"

	"my.favorite/cclawservice"
	"my.favorite/platformutility/logging"
)

//	@title			企业级AI助手操作系统
//	@version		1.0.0
//	@description	API服务入口

//	@host		localhost:8443
//	@BasePath	/

// @securityDefinitions.apikey	ApiKeyAuth
// @in							header
// @name						AUTHORIZATION
func main() {
	// 开启运行界面
	////tui_engine.GetLogManager().Start()
	//
	versionString := VersionString
	logging.LogInfof(versionString)

	configFileName := GetConfigFileName()
	mainApp := cclawservice.NewMainApp(configFileName)
	if !mainApp.Init() {
		mainApp.Destroy()
		return
	}

	// 添加 health controller
	if !mainApp.SetupHealthController() {
		mainApp.Destroy()
		return
	}

	if !mainApp.SetupLoggingController() {
		mainApp.Destroy()
		return
	}

	// 1. setup server
	if !mainApp.SetupServer() {
		mainApp.Destroy()
		return
	}

	// 2. start the server
	mainApp.Startup()

	// 3. wait the exit signal
	mainApp.Shutdown()

	mainApp.Destroy()

	//// 结束TUI
	//tui_engine.GetLogManager().Stop()
}
