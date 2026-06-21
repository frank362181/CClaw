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
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	instance *AsyncLogger
	once     sync.Once
)

func GetAsyncLogger(outer io.Writer, flushInterval int, bufferSize int, format string) *AsyncLogger {
	once.Do(func() {
		instance = buildAsyncLoggingWriter(outer, flushInterval, bufferSize, format)
	})

	return instance
}

// Close 优雅关闭,刷新日志
func (al *AsyncLogger) Close() {
	close(al.flushChannel)
	al.waitGroup.Wait()
	al.sync()
	al.Logger.Sync()
}

// ============================= private functions & methods =======================================
func buildAsyncLoggingWriter(outer io.Writer, flushInterval int, bufferSize int, format string) *AsyncLogger {
	// 创建底层写入目标
	writer := zapcore.AddSync(outer)

	// 包装为缓冲写入器
	bufferWriter := &zapcore.BufferedWriteSyncer{
		WS:            writer,
		Size:          bufferSize,
		FlushInterval: 0,
	}

	// encoding setting
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "time"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderCfg.MessageKey = "message"
	encoderCfg.LevelKey = "level"

	var encoder zapcore.Encoder
	if format == "json" {
		encoder = zapcore.NewJSONEncoder(encoderCfg)
	} else {
		encoder = zapcore.NewConsoleEncoder(encoderCfg)
	}

	// 4. 仅仅记录INFO以上级别的日志
	core := zapcore.NewCore(encoder, bufferWriter, zapcore.InfoLevel)

	// 5. 构建 Logger
	logger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	al := &AsyncLogger{
		Logger:       logger,
		writeSyncer:  writer,
		flushChannel: make(chan struct{}),
	}

	// 6 后台启动goroutine定期刷缓冲区
	if flushInterval > 0 {
		al.waitGroup.Add(1)
		go al.periodicFlush(flushInterval)
	}

	return al
}

func (al *AsyncLogger) periodicFlush(flushInterval int) {
	defer al.waitGroup.Done()
	ticker := time.NewTicker(time.Duration(flushInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			al.Sync()
		case <-al.flushChannel:
			al.Sync()
			return
		}
	}
}

func (al *AsyncLogger) sync() {
	if err := al.writeSyncer.Sync(); err != nil {
		// 极少情况下出错,兜底
		fmt.Fprintf(os.Stderr, "async logger sync error: %v\n", err)
	}
}
