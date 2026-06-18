/*
定义日志的常量

Author:
	Frank Ng
Date:
	Jun 18, 2026
*/

package logging

import "strings"

type LogLevel int //define the level of the log

const (
	DEBUG LogLevel = iota
	INFO
	WARNING
	ERROR
	FETAL
)

var levelNames = map[LogLevel]string{
	DEBUG:   "DEBUG",
	INFO:    "INFO",
	WARNING: "WARN",
	ERROR:   "ERROR",
	FETAL:   "FATAL",
}

// ParseLogLevel 将字符串解析为 LogLevel
func ParseLogLevel(s string) LogLevel {
	switch strings.ToUpper(s) {
	case "DEBUG":
		return DEBUG
	case "INFO":
		return INFO
	case "WARNING":
		return WARNING
	case "ERROR":
		return ERROR
	case "FETAL":
		return FETAL
	default:
		return INFO
	}
}
