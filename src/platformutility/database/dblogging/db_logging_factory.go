package dblogging

import (
	"log"
	"strings"
	"time"

	"gorm.io/gorm/logger"
)

func GetDbLogger(levelStr string) logger.Interface {
	return logger.New(
		log.New(DbLogWriter{}, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             50 * time.Millisecond,
			LogLevel:                  getLoggingLevel(levelStr),
			IgnoreRecordNotFoundError: true, //登录查无用户等“无记录”不再当错误刷屏
			Colorful:                  false,
		},
	)
}

func getLoggingLevel(s string) logger.LogLevel {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "silent":
		return logger.Silent
	case "error":
		return logger.Error
	case "info":
		return logger.Info
	default:
		return logger.Warn
	}
}
