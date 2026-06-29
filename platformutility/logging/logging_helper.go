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
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/logging/loggingwriter"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
)

// 定义标准输出的日志输出
var stdoutInstance = common.GetSingleton(func() *loggingwriter.AsyncLogger {
	if common.IsWindows() {

		//w := &loggingwriter.ConsoleWriter{ Writer: os.Stdout}
		//return loggingwriter.OpenLogger(w, 1, 256*1024, "json")
	}

	var tempIo io.Writer = os.Stdout
	temp := &loggingwriter.ConsoleWriter{Writer: tempIo}
	return loggingwriter.OpenLogger(temp, 1, 256*1024, "json")
}).Instance()

// 定义文件日志输出
var (
	logDir = common.GetOSUtilityInstance().GetCurrentDir() + "/log/"
	_      = common.CreateDir(logDir)

	writer, _ = rotatelogs.New(
		logDir+"%Y%m%d%H%M.log",
		rotatelogs.WithRotationTime(24*time.Hour),
		rotatelogs.WithMaxAge(7*24*time.Hour),
	)

	fileInstance = common.GetSingleton(func() *loggingwriter.AsyncLogger {
		return loggingwriter.OpenLogger(writer, 1, 256*1024, "json")
	}).Instance()
)

func CloseLogger() {
	if stdoutInstance != nil {
		stdoutInstance.Close()
	}

	if fileInstance != nil {
		fileInstance.Close()
	}
}

func LogDebug(v ...interface{}) {
	if len(v) == 0 {
		return
	}

	msg := formatLog(v)

	if stdoutInstance != nil {
		stdoutInstance.Debug(msg)
	}

	if fileInstance != nil {
		fileInstance.Debug(msg)
	}
}

func LogInfo(v ...interface{}) {
	if len(v) == 0 {
		return
	}

	msg := formatLog(v)

	if stdoutInstance != nil {
		stdoutInstance.Info(msg)
	}

	if fileInstance != nil {
		fileInstance.Info(msg)
	}
}

func LogWarn(v ...interface{}) {
	if len(v) == 0 {
		return
	}

	msg := formatLog(v)
	if stdoutInstance != nil {
		stdoutInstance.Warn(msg)
	}
	if fileInstance != nil {
		fileInstance.Warn(msg)
	}
}

func LogError(v ...interface{}) {
	if len(v) == 0 {
		return
	}

	msg := formatLog(v)
	if stdoutInstance != nil {
		stdoutInstance.Error(msg)
	}
	if fileInstance != nil {
		fileInstance.Error(msg)
	}
}

func LogFatal(v ...interface{}) {
	if len(v) == 0 {
		return
	}
	msg := formatLog(v)
	if stdoutInstance != nil {
		stdoutInstance.Fatal(msg)
	}
	if fileInstance != nil {
		fileInstance.Fatal(msg)
	}
}

// ======================================== private functions & methods ====================================
func formatLog(v ...interface{}) string {
	var msg string
	if format, ok := v[0].(string); ok && strings.Contains(format, "%") {
		msg = fmt.Sprintf(format, v[1:]...)
	} else {
		msg = fmt.Sprint(v...)
	}

	_, file, line, ok := runtime.Caller(2) //跳过2层到真正调用打印日志的调用方所在文件
	if !ok {
		return msg
	}

	// 提取文件名,去掉路径
	short := file
	for i := len(file) - 1; i > 0; i-- {
		if file[i] == '/' || file[i] == '\\' {
			short = file[i+1:]
			break
		}
	}
	msg = fmt.Sprintf("%s:%d %s", short, line, msg)
	return msg
}
