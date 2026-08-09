package dblogging

import (
	"strings"

	"my.favorite/platformutility/logging"
)

func (w DbLogWriter) Write(log []byte) (int, error) {
	logging.LogInfoF("%s", strings.TrimRight(string(log), "\n"))
	return len(log), nil
}
