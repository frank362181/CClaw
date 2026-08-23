package main

import (
	"path/filepath"

	_ "my.favorite/agentService/docs"
	"my.favorite/cclawservice"
	"my.favorite/platformutility/common"

	"my.favorite/platformutility/logging"
)

const (
	projectName    = "agentService"
	configFileName = "agentService.ini"
)

// GetProjectDir 返回当前项目的目录（基于工作目录，兼容开发期从源码根运行）
func getProjectDir() string {
	projectDir := filepath.Join(common.GetCurrentDir(), projectName)
	return projectDir
}

// GetConfigDir 返回基于工作目录的配置目录：<CWD>/agentService/config
func getConfigDir() string {
	return filepath.Join(getProjectDir(), "config")
}

// GetConfigFileName 返回配置文件的完整路径。按优先级依次探测：
//  1. <可执行文件目录>/config/agentService.ini               —— 部署到 bin（bin\config）
//  2. <可执行文件目录>/agentService/config/agentService.ini  —— 部署到 bin 且保留工程目录结构
//  3. <CWD>/agentService/config/agentService.ini             —— 开发期从源码根运行
//
// 返回第一个存在的文件；都不存在时返回第 1 个候选，便于上层报错信息清晰可读。
func getConfigFileName() string {
	exeDir := common.GetExecutableDir()
	candidates := []string{
		filepath.Join(exeDir, "config", configFileName),
		filepath.Join(exeDir, projectName, "config", configFileName),
		filepath.Join(getConfigDir(), configFileName),
	}

	for _, candidate := range candidates {
		if common.FileExists(candidate) {
			return candidate
		}
	}

	return candidates[0]
}

/*==================================================
*                  应用入口函数
**/

//	@title			企业级AI助手操作系统
//	@version		1.0.0
//	@description	API服务入口

//	@host		localhost:8443
//	@BasePath	/

// @securityDefinitions.apikey	ApiKeyAuth
// @in							header
// @name						AUTHORIZATION
func main() {
	versionString := VersionString
	logging.LogInfoF(versionString)

	configFileName := getConfigFileName()
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

	// 添加 agent 管理控制器(须在登录控制器之后安装,以复用认证 interceptor)
	if err := mainApp.SetupAgentController(); err != nil {
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
}
