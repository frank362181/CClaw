/*
@Summary:
	异步日志输出工具
@Date:
	18 Jun, 2026
@Author:
	Frank Ng
*/

package logging

import (
	"os"
	"time"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/logging/loggingwriter"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
)

// 定义标准输出的日志输出
var stdoutInstance = common.GetSingleton(func() *loggingwriter.AsyncLogger {
	return loggingwriter.OpenLogger(os.Stdout, 1, 256*1024, "json")
}).Instance()

// 定义文件日志输出
var (
	logDir = common.GetOSUtilityInstance().GetCurrentDir() + "/log"
	err    = common.GetOSUtilityInstance().CreateDir(logDir)

	writer, _ = rotatelogs.New(
		logDir+"%Y%m%d%H%M.log",
		rotatelogs.WithRotationTime(24*time.Hour),
		rotatelogs.WithMaxAge(7*24*time.Hour),
	)

	fileInstance = common.GetSingleton(func() *loggingwriter.AsyncLogger {
		return loggingwriter.OpenLogger(writer, 1, 256*1024, "json")
	}).Instance()
)
