package logging

import "os"

type LoggerStdOutWriter struct{}

func (w *LoggerStdOutWriter) Write(p []byte) (n int, err error) {
	return os.Stdout.Write(p)
}

func (w *LoggerStdOutWriter) Close() error {
	return nil
}
