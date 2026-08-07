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

	defer mainApp.Destroy()

	if err := mainApp.Init(); err != nil {
		return
	}

	// 添加 health controller
	if err := mainApp.SetupHealthController(); err != nil {
		return
	}

	if err := mainApp.SetupLoginController(); err != nil {
		return
	}

	// 1. setup server
	if err := mainApp.SetupServer(); err != nil {
		return
	}

	// 2. start the server
	_ = mainApp.Startup()

	// 3. wait the exit signal
	mainApp.Shutdown()

	//// 结束TUI
	//tui_engine.GetLogManager().Stop()
}
