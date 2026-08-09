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
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/logging/loggingwriter"
	tuiengine "my.favorite/tui-engine/engine"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
)

// 定义标准输出的日志输出
var stdoutInstance = common.GetSingleton(func() *loggingwriter.AsyncLogger {
	//if common.IsWindows() {
	//	w := &loggingwriter.ConsoleWriter{File: os.Stdout}
	//	return loggingwriter.OpenLogger(w, 1, 256*1024, "json")
	//}

	var w = &loggingwriter.ConsoleWriter{File: os.Stdout}
	asyncLogger := loggingwriter.OpenLogger(w, 1, 256*1024, "json")

	logMgr := tuiengine.GetLogManager()
	asyncLogger.SetLogMgr(logMgr)

	return asyncLogger
}).Instance()

// 定义文件日志输出
var (
	logDir = filepath.Join(common.GetExecutableDir(), "log")
	_      = common.CreateDir(logDir)

	logFileName = filepath.Join(logDir, "%Y%m%d%H%M.log")
	writer, _   = rotatelogs.New(
		logFileName,
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

// LogDebugF 格式化日志输出(Debug级别),名称以f结尾以便go vet识别为Printf风格函数
func LogDebugF(format string, args ...interface{}) {
	if len(format) == 0 {
		return
	}

	_, file, line, _ := runtime.Caller(1)
	logDebug(file, line, append([]interface{}{format}, args...)...)
}

// LogInfoF 格式化日志输出(Info级别),名称以f结尾以便go vet识别为Printf风格函数
func LogInfoF(format string, args ...interface{}) {
	if len(format) == 0 {
		return
	}

	_, file, line, _ := runtime.Caller(1)
	logInfo(file, line, append([]interface{}{format}, args...)...)
}

// LogWarnF 格式化日志输出(Warn级别),名称以f结尾以便go vet识别为Printf风格函数
func LogWarnF(format string, args ...interface{}) {
	if len(format) == 0 {
		return
	}

	_, file, line, _ := runtime.Caller(1)
	logWarn(file, line, append([]interface{}{format}, args...)...)
}

// LogErrorF 格式化日志输出(Error级别),名称以f结尾以便go vet识别为Printf风格函数
func LogErrorF(format string, args ...interface{}) {
	if len(format) == 0 {
		return
	}

	_, file, line, _ := runtime.Caller(1)
	logError(file, line, append([]interface{}{format}, args...)...)
}

// LogFatalF 格式化日志输出(Fatal级别),名称以f结尾以便go vet识别为Printf风格函数
func LogFatalF(format string, args ...interface{}) {
	if len(format) == 0 {
		return
	}

	_, file, line, _ := runtime.Caller(1)
	logFatal(file, line, append([]interface{}{format}, args...)...)
}

// ======================================== private functions & methods ====================================

func logDebug(file string, line int, v ...interface{}) {
	if len(v) == 0 {
		return
	}

	msg := formatLog(file, line, v...)

	if stdoutInstance != nil {
		//stdoutInstance.Debug(msg)
		if stdoutInstance.GetLogMgr() != nil {
			stdoutInstance.GetLogMgr().Log(levelNames[DEBUG], msg)
		}
	}

	if fileInstance != nil {
		fileInstance.Debug(msg)
	}
}

func logInfo(file string, line int, v ...interface{}) {
	if len(v) == 0 {
		return
	}

	msg := formatLog(file, line, v...)

	if stdoutInstance != nil {
		//stdoutInstance.Info(msg)
		if stdoutInstance.GetLogMgr() != nil {
			stdoutInstance.GetLogMgr().Log(levelNames[INFO], msg)
		}
	}

	if fileInstance != nil {
		fileInstance.Info(msg)
	}
}

func logWarn(file string, line int, v ...interface{}) {
	if len(v) == 0 {
		return
	}

	msg := formatLog(file, line, v...)
	if stdoutInstance != nil {
		//stdoutInstance.Warn(msg)
		if stdoutInstance.GetLogMgr() != nil {
			stdoutInstance.GetLogMgr().Log(levelNames[WARNING], msg)
		}
	}
	if fileInstance != nil {
		fileInstance.Warn(msg)
	}
}

func logError(file string, line int, v ...interface{}) {
	if len(v) == 0 {
		return
	}

	msg := formatLog(file, line, v...)
	if stdoutInstance != nil {
		//stdoutInstance.Error(msg)
		if stdoutInstance.GetLogMgr() != nil {
			stdoutInstance.GetLogMgr().Log(levelNames[ERROR], msg)
		}
	}
	if fileInstance != nil {
		fileInstance.Error(msg)
	}
}

func logFatal(file string, line int, v ...interface{}) {
	if len(v) == 0 {
		return
	}
	msg := formatLog(file, line, v...)
	if stdoutInstance != nil {
		//stdoutInstance.Fatal(msg)
		if stdoutInstance.GetLogMgr() != nil {
			stdoutInstance.GetLogMgr().Log(levelNames[FETAL], msg)
		}
	}
	if fileInstance != nil {
		fileInstance.Fatal(msg)
	}
}

func formatLog(file string, line int, v ...interface{}) string {
	var msg string
	if format, ok := v[0].(string); ok && strings.Contains(format, "%") {
		msg = fmt.Sprintf(format, v[1:]...)
	} else {
		msg = fmt.Sprint(v...)
	}

	// 提取短文件名（去掉路径），兼容 Windows 反斜杠与 Linux 正斜杠
	short := filepath.Base(file)
	if short == "." || short == string(filepath.Separator) {
		short = file
	}
	msg = fmt.Sprintf("%s:%d %s", short, line, msg)
	return msg
}
