/*******************************************************
* @Summary:
*       退避(backoff)工具:指数退避 + 抖动(jitter)。
*
*       用于"可重试的瞬时故障"的重试等待。抖动用于打散并发调用方的重试时间点,
*       避免所有调用方在同一时刻同步重试(惊群 / thundering herd);
*       采用"等比例抖动",保留退避量级,不会把等待缩到接近 0。
* @Author:
*       Frank Ng
* @Date:
*       Sep 10, 2026
*******************************************************/

package common

import (
	"math/rand/v2"
	"time"
)

// DefaultJitterRatio 默认抖动比例(±20%):保留退避量级的前提下打散并发重试。
const DefaultJitterRatio = 0.2

// maxBackoffDelay 翻倍计算的防溢出上限(约 146 年),仅在 BaseDelay 异常大时兜底。
const maxBackoffDelay = time.Duration(1) << 62

// BackoffPolicy 指数退避策略。
//   - 基础序列:delay = BaseDelay * 2^(retryIndex-1)(retryIndex 从 1 开始)
//   - 上限:MaxDelay > 0 时封顶
//   - 抖动:在封顶后的 delay 上叠加 ±JitterRatio 的等比例随机量
//   - BaseDelay <= 0 表示禁用退避(恒返回 0);JitterRatio <= 0 表示不抖动
type BackoffPolicy struct {
	BaseDelay   time.Duration // 基准间隔(第 1 次重试的等待时长)
	MaxDelay    time.Duration // 单次等待上限(<=0 表示不设上限)
	JitterRatio float64       // 抖动比例,取值 [0,1];推荐 DefaultJitterRatio
}

// Delay 计算第 retryIndex 次重试的等待时长(retryIndex 从 1 开始)。
// 返回值已包含抖动,因此可能略大于 MaxDelay(最多 MaxDelay*(1+JitterRatio))。
func (p BackoffPolicy) Delay(retryIndex int) time.Duration {
	if p.BaseDelay <= 0 || retryIndex <= 0 {
		return 0
	}

	delay := p.BaseDelay
	for i := 1; i < retryIndex; i++ {
		if p.MaxDelay > 0 && delay >= p.MaxDelay {
			break // 已达上限,无需继续翻倍
		}
		if delay > maxBackoffDelay {
			break // 防溢出:再翻倍会溢出
		}
		delay *= 2
	}
	if p.MaxDelay > 0 && delay > p.MaxDelay {
		delay = p.MaxDelay
	}

	return ApplyJitter(delay, p.JitterRatio)
}

// ApplyJitter 对 delay 施加等比例随机抖动:结果落在 [delay*(1-ratio), delay*(1+ratio)]。
// 该方式保留退避量级(不会把等待缩到接近 0),仅把并发调用方的时间点打散。
// delay<=0 或 ratio<=0 时原样返回;ratio>1 时按 1 处理。
func ApplyJitter(delay time.Duration, ratio float64) time.Duration {
	if delay <= 0 || ratio <= 0 {
		return delay
	}
	if ratio > 1 {
		ratio = 1
	}
	// rand.Float64() ∈ [0,1) → factor ∈ [1-ratio, 1+ratio)
	factor := 1 + (rand.Float64()*2-1)*ratio
	jittered := time.Duration(float64(delay) * factor)
	if jittered < 0 {
		return 0
	}
	return jittered
}

// SecondsToDuration 秒(float,可含小数) → time.Duration;<=0 返回 0。
// 用于把配置中的秒数(如 retry_max_delay=60.0)统一换算为时长。
func SecondsToDuration(seconds float64) time.Duration {
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}
