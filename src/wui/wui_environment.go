/*
* 当前应用关联的环境变量以及一些公共的环境变量
* 该环境类的代码适用于wui项目，其他项目请不要使用该类
 */

package main

import (
	"path/filepath"

	"my.favorite/platformutility/common"
)

const (
	projectName    = "wui"
	configFileName = "cclaw.ini"
)

func GetProjectDir() string {
	curDir := common.GetCurrentDir()
	projectDir := filepath.Join(curDir, projectName)
	return projectDir
}

/*
* 获取配置文件所在的目录
**/
func GetConfigDir() string {
	return filepath.Join(GetProjectDir(), "config")
}

/*
* 获取配置文件的完整路径
**/
func GetConfigFileName() string {
	return filepath.Join(GetConfigDir(), configFileName)
}
