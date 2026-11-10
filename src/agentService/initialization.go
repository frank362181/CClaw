package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"

	"my.favorite/platformutility"
	"my.favorite/platformutility/common"
)

var (
	Version   = "3.0.0"
	Commit    = strings.Join([]string{"feature 1:coordinator-worker架构支持.\n"}, platformutility.Commit)
	BuildTime = common.GetDatetimeUtilityInstance().GetCurrentDatetime().DateStr

	GoVersion = runtime.Version() //运行时自动获取
)

var (
	VersionString = getVersion()
)

func init() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	//主模块版本（最终二进制的程序）
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		Version = info.Main.Version
	}

	// 从vcs 信息中提取commit 和 时间
	if Version == "" || Version == "(devel)" {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if len(s.Value) >= 7 {
					Version = s.Value[:7]
				}
			case "vcs.time":
				BuildTime = s.Value
			}
		}
	}

	//如果库需要的是自身的版本,需要遍历Deps找到自己
	const selfModulePath = "my.favorite/agentService" //库的模块路径
	for _, dep := range info.Deps {
		if dep.Path == selfModulePath {
			if dep.Version != "" {
				Version = dep.Version
			}
			break
		}
	}
}

func getVersion() string {
	verString := fmt.Sprintf("Version: %s\nCommit: %s\nBuildTime: %s \nGoVersion: %s\nOS/Arch: %s/%s",
		Version, Commit, BuildTime, GoVersion, runtime.GOOS, runtime.GOARCH)

	return verString
}
