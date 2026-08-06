package dblogging

import (
	"strings"

	"my.favorite/platformutility/logging"
)

func (w DbLogWriter) Write(log []byte) (int, error) {
	logging.LogInfof("%s", strings.TrimRight(string(log), "\n"))
	return len(log), nil
}
