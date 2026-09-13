/**
* 压缩节流
*
* 职责:按会话限制"压缩尝试"的频率,避免降级后每轮重试把后台 LLM 调用打满。
*
* 要解决的具体抖动:
*   - 保全校验失败时,压缩只覆盖区间前半,后半的原文继续以原文回放;
*   - 但后半下一轮又会被预算裁掉 → 又满足压缩条件 → 又一次 LLM 调用;
*   - 若该会话内容本身就"摘要容易丢要点",就退化成每轮一次压缩。
*
* 策略:
*   - 一次尝试后进入冷却(基数 CompactionCooldownSeconds);
*   - 连续降级按 2 倍退避(上限 CompactionMaxCooldownSeconds),成功即清零;
*   - 只跳过"压缩尝试"本身,不影响裁剪与对话:这仍是一条旁路增强。
**/

package mstorage

import (
	"sync"

	pCommon "my.favorite/platformutility/common"
)

// 节流参数
const (
	// CompactionCooldownSeconds 一次正常尝试后的最短冷却(秒):同一会话不会连续两轮都压缩。
	CompactionCooldownSeconds = 60
	// compactionDegradeBackoffSeconds 连续降级的退避基数(秒):第 n 次连续降级后等待 base<<(n-1)。
	compactionDegradeBackoffSeconds = 120
	// CompactionMaxCooldownSeconds 退避上限(秒):再糟也不会超过该间隔。
	CompactionMaxCooldownSeconds = 3600
	// compactionThrottleMaxEntries 节流表容量上限;超出整体重置(与其它缓存一致,内存有界)。
	compactionThrottleMaxEntries = 8192
)

// compactionThrottleEntry 一个会话的节流状态。
type compactionThrottleEntry struct {
	lastAttemptAt int64 // 上次尝试时刻(Unix 秒);0 表示尚未尝试
	degradedCount int   // 连续降级次数(成功的尝试会清零)
}

// CompactionThrottle 压缩节流表(线程安全)。
type CompactionThrottle struct {
	mu      sync.Mutex
	entries map[string]*compactionThrottleEntry
}

var compactionThrottle = pCommon.GetSingleton(func() *CompactionThrottle {
	return &CompactionThrottle{entries: make(map[string]*compactionThrottleEntry)}
}).Instance()

// GetCompactionThrottle 返回全局唯一的压缩节流表。
func GetCompactionThrottle() *CompactionThrottle {
	return compactionThrottle
}

// Allow 报告该会话当前是否允许发起压缩尝试。
func (t *CompactionThrottle) Allow(sessionId string, now int64) bool {
	t.mu.Lock()
	entry := t.entries[sessionId]
	last, degraded := int64(0), 0
	if entry != nil {
		last, degraded = entry.lastAttemptAt, entry.degradedCount
	}
	t.mu.Unlock()

	if last <= 0 {
		return true // 从未尝试过
	}
	return now-last >= cooldownSeconds(degraded)
}

// RecordSuccess 记录一次成功的尝试(摘要已落盘);degraded 表示这次是降级覆盖。
func (t *CompactionThrottle) RecordSuccess(sessionId string, now int64, degraded bool) {
	t.record(sessionId, now, degraded, true)
}

// RecordFailure 记录一次失败的尝试(摘要器报错/摘要为空):只更新时刻,
// 保留连续降级计数 —— 失败既不该延长退避,也不该被当成"恢复了"。
func (t *CompactionThrottle) RecordFailure(sessionId string, now int64) {
	t.record(sessionId, now, false, false)
}

// Reset 清除某会话的节流状态(测试与运维用)。
func (t *CompactionThrottle) Reset(sessionId string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.entries, sessionId)
}

// ============================== private functions & methods ==============================

// record 记录一次尝试。
//   - countDegraded=true:按本次结果更新连续降级计数(成功清零);
//   - countDegraded=false:只更新尝试时刻。
func (t *CompactionThrottle) record(sessionId string, now int64, degraded, countDegraded bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.entries == nil { // 兼容直接构造的结构体(单测)
		t.entries = make(map[string]*compactionThrottleEntry)
	}
	if _, ok := t.entries[sessionId]; !ok && len(t.entries) >= compactionThrottleMaxEntries {
		t.entries = make(map[string]*compactionThrottleEntry)
	}

	entry := t.entries[sessionId]
	if entry == nil {
		entry = &compactionThrottleEntry{}
		t.entries[sessionId] = entry
	}
	entry.lastAttemptAt = now

	if !countDegraded {
		return
	}
	if degraded {
		entry.degradedCount++
	} else {
		entry.degradedCount = 0
	}
}

// cooldownSeconds 计算冷却时长:正常尝试为基数;连续降级按 2 倍退避并封顶。
func cooldownSeconds(degradedCount int) int64 {
	cooldown := int64(CompactionCooldownSeconds)
	if degradedCount <= 0 {
		return cooldown
	}

	backoff := int64(compactionDegradeBackoffSeconds)
	for i := 1; i < degradedCount && backoff < CompactionMaxCooldownSeconds; i++ {
		backoff *= 2
	}
	if backoff > CompactionMaxCooldownSeconds {
		backoff = CompactionMaxCooldownSeconds
	}
	if backoff > cooldown {
		cooldown = backoff
	}
	return cooldown
}
