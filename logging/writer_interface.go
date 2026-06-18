/*
@Summary:
	定义日志输出的接口
@Date:
	Jun 18, 2026
@Author:
	Frank Ng
*/

package logging

import "io"

type LoggerWriter interface {
	io.Writer
	Close() error
}
