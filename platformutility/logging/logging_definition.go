/*
@Summary:
	定义日志输出的接口
@Date:
	Jun 18, 2026
@Author:
	Frank Ng
*/

package logging

import (
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type AsyncLogger struct {
	*zap.Logger
	writeSyncer  zapcore.WriteSyncer //底层写入器（用于关闭同步）
	flushChannel chan struct{}       //用于主动触发刷新
	waitGroup    sync.WaitGroup
}
