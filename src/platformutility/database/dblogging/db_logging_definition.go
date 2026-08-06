package dblogging

// DbLogWriter 日志写入工具
type DbLogWriter struct{}

type ILoggingWriter interface {
	Write(log []byte) (int, error)
}
