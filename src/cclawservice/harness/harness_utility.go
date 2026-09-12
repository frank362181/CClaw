/**
* 流式读取失败的归一化:把底层读流错误分类为"客户端断开"或"传输层故障"。
*
* 归属 harness(而非 llmproto):这些判定依赖客户端的读循环策略 —— 空闲超时阈值、
* 外层 ctx 与内层 streamCtx 的分层语义,属客户端行为而非上游协议。
**/

package harness

import (
	"context"
	"fmt"
	"time"

	"my.favorite/cclawservice/llmproto"
	pException "my.favorite/platformutility/exception"
)

// streamReadError 归一化流读取失败:
//   - 客户端断开(外层 ctx 取消)      → ErrRequestCanceled(静默,不重试)
//   - 空闲超时(仅内层 streamCtx 取消) → 传输层错误(可重试)
//   - 其它(连接重置/EOF 等)          → 传输层错误(可重试)
func streamReadError(ctx context.Context, streamCtx context.Context, err error, requestID string, idle time.Duration) error {
	if ctx.Err() != nil {
		return llmproto.ErrRequestCanceled
	}
	if idle > 0 && streamCtx.Err() != nil {
		return llmproto.NewLlmStreamTransportError(
			fmt.Errorf("stream idle timeout after %s without data: %v", idle, err), requestID)
	}
	if pException.IsContextCanceled(streamCtx, err) {
		return llmproto.ErrRequestCanceled
	}
	return llmproto.NewLlmStreamTransportError(err, requestID)
}
