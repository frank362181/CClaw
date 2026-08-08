/*
* 当前应用关联的环境变量以及一些公共的环境变量
 */

package main

import (
	"os"
	"path/filepath"

	"my.favorite/platformutility/common"
)

const (
	projectName    = "agentService"
	configFileName = "cclaw.ini"
)

// GetExecutableDir 返回可执行文件所在目录（解析符号链接后的真实目录）。
// 部署场景下配置文件放置在可执行文件附近，据此定位最可靠，且与启动时的工作目录无关。
func GetExecutableDir() string {
	exePath, err := os.Executable()
	if err != nil {
		panic(err)
	}

	realPath, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		realPath = exePath
	}

	return filepath.Dir(realPath)
}

// GetProjectDir 返回当前项目的目录（基于工作目录，兼容开发期从源码根运行）
func GetProjectDir() string {
	projectDir := filepath.Join(common.GetCurrentDir(), projectName)
	return projectDir
}

// GetConfigDir 返回基于工作目录的配置目录：<CWD>/agentService/config
func GetConfigDir() string {
	return filepath.Join(GetProjectDir(), "config")
}

// GetConfigFileName 返回配置文件的完整路径。按优先级依次探测：
//  1. <可执行文件目录>/config/cclaw.ini               —— 部署到 bin（bin\config）
//  2. <可执行文件目录>/agentService/config/cclaw.ini  —— 部署到 bin 且保留工程目录结构
//  3. <CWD>/agentService/config/cclaw.ini             —— 开发期从源码根运行
//
// 返回第一个存在的文件；都不存在时返回第 1 个候选，便于上层报错信息清晰可读。
func GetConfigFileName() string {
	exeDir := GetExecutableDir()
	candidates := []string{
		filepath.Join(exeDir, "config", configFileName),
		filepath.Join(exeDir, projectName, "config", configFileName),
		filepath.Join(GetConfigDir(), configFileName),
	}

	for _, candidate := range candidates {
		if common.FileExists(candidate) {
			return candidate
		}
	}

	return candidates[0]
}
