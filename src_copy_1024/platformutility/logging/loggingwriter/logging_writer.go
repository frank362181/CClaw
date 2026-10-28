package loggingwriter

import (
	"fmt"
	"io"
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func OpenLogger(outer io.Writer, flushInterval int, bufferSize int, format string) *AsyncLogger {
	return buildAsyncLoggingWriter(outer, flushInterval, bufferSize, format)
}

// Close 优雅关闭,刷新日志
func (al *AsyncLogger) Close() {
	close(al.flushChannel)
	al.waitGroup.Wait()
	al.sync()
	_ = al.Logger.Sync()
}

// ============================= private functions & methods =======================================
func buildAsyncLoggingWriter(outer io.Writer, flushInterval int, bufferSize int, format string) *AsyncLogger {
	// 创建底层写入目标
	writer := zapcore.AddSync(outer)

	// 包装为缓冲写入器
	bufferWriter := &zapcore.BufferedWriteSyncer{
		WS:            writer,
		Size:          bufferSize,
		FlushInterval: 0, //关闭了自刷新
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
	core := zapcore.NewCore(encoder, bufferWriter, zapcore.DebugLevel)

	// 5. 构建 Logger
	logger := zap.New(core, zap.AddStacktrace(zapcore.ErrorLevel))

	al := &AsyncLogger{
		Logger:       logger,
		writeSyncer:  bufferWriter,
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
			al.sync()
		case <-al.flushChannel:
			al.sync()
			return
		}
	}
}

func (al *AsyncLogger) sync() {
	if err := al.writeSyncer.Sync(); err != nil {
		// 极少情况下出错,兜底
		_, err := fmt.Fprintf(os.Stderr, "async logger sync error: %v\n", err)
		if err != nil {
			return
		}
	}
}
