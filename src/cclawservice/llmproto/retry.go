/**
* 重试判定与取消哨兵。
*
* ErrRequestCanceled 放在本包而非 exception:它是**传输层**语义(客户端断开/上下文超时),
* 且 IsRetryableLlmError 需要判定它;若留在业务错误包会与 exception → llmproto 形成循环依赖。
**/

package llmproto

import (
	"context"
	"errors"
)

// ErrRequestCanceled 表示请求上下文被取消(客户端断开/超时),而非上游故障。
var ErrRequestCanceled = errors.New("runtime client: request context canceled")

// IsRetryableLlmError 判定错误是否可安全重试(瞬时故障)。
//   - 传输层故障(连接失败/断流;StatusCode==0)可重试;
//   - 真限流(429)与真服务端故障(5xx)可重试;
//   - HTTP 200 携带 error 体的应用级错误、客户端取消、配置类错误(鉴权/模型名/参数/欠费/内容合规)不重试。
func IsRetryableLlmError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrRequestCanceled) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if statusErr, ok := errors.AsType[*LlmStatusError](err); ok {
		switch statusErr.Category {
		case LlmCategoryRateLimit:
			return true
		case LlmCategoryServer:
			return statusErr.StatusCode == 0 || statusErr.StatusCode >= 500
		}
	}
	return false
}
