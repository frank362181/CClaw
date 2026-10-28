/**
* 定义重试的类:基于指定的重试策略进行重试
**/

package chat

import (
	"context"
	"time"

	"my.favorite/cclawservice/common/llmproto"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"
)

// RetryPolicy 可重试错误(429/5xx/断流)的指数退避策略
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Jitter      float64
}

func DefaultRetryPolicy(maxAttempts int, baseDelay int64, maxDelay int64, jitterRatio float64) RetryPolicy {
	return RetryPolicy{
		MaxAttempts: maxAttempts,                                 //hCommon.LlmRetryMaxAttempts,
		BaseDelay:   time.Duration(baseDelay) * time.Millisecond, //time.Duration(hCommon.LlmRetryBaseDelayMs) * time.Millisecond,
		MaxDelay:    time.Duration(maxDelay) * time.Millisecond,  //time.Duration(hCommon.LlmRetryMaxDelayMs) * time.Millisecond,
		Jitter:      jitterRatio,                                 //hCommon.LlmRetryJitterRatio,
	}
}

func (r *RetryPolicy) Attempts() int {
	if r.MaxAttempts <= 0 {
		return 1 // 夹取:否则 for 一次也不会跑,返回 (0/nil)会被当成"成功但内容为空"
	}

	return r.MaxAttempts
}

// 在第 nextAttempt次尝试前退避等待(nextAttempt从第2次开始）
func (r *RetryPolicy) wait(ctx context.Context, nextAttempt int, lastErr error) error {
	// 已取消就没必要再睡了:直接返回,避免多打一条误导性重试日志
	if err := ctx.Err(); err != nil {
		return llmproto.ErrRequestCanceled
	}

	// delay 的retry从 1 开始
	delay := pCommon.RetryBackPolicy(r.BaseDelay, r.MaxDelay, r.Jitter).Delay(nextAttempt - 1)
	logging.LogWarnF("runtime client: retry %d/%d after %s,last error:%s", nextAttempt, r.Attempts(), delay, lastErr.Error())

	if delay <= 0 {
		return nil // 退避被显式禁用
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return llmproto.ErrRequestCanceled
	case <-timer.C: // 超时
		return nil
	}
}

// Retry 执行一次可能重试的调用
// op返回 emitted=true 表示 **已经向调用房交付过不可能撤回的增量**
func Retry[T any](ctx context.Context, policy RetryPolicy, op func(ctx context.Context) (T, bool, error)) (T, error) {
	var (
		zero    T
		lastErr error
	)

	attempts := policy.Attempts()
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			if err := policy.wait(ctx, attempt, lastErr); err != nil {
				return zero, err
			}
		}

		res, emitted, err := op(ctx)
		if err == nil {
			return res, nil
		}

		lastErr = err
		if emitted || !llmproto.IsRetryableLlmError(lastErr) {
			return res, lastErr
		}
	}

	return zero, lastErr
}
