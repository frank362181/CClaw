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

package ctxmgr

import (
	"sort"
	"strings"
	"sync"

	pCommon "my.favorite/platformutility/common"
)

// 节流参数
const (
	// CompactionProgressCooldownSeconds **已推进**(摘要已落盘且未降级)后的冷却(秒)。
	// 刻意很短:冷却的本意是抑制"降级→下轮又裁→再压"的抖动,不是给正常推进限速。
	// 大区间分批压是常态(见 fitCompactionRange),冷却太长会让积压排空需要几分钟。
	CompactionProgressCooldownSeconds = 5
	// CompactionCooldownSeconds 一次**失败**尝试后的冷却(秒):上游可能还在报错,不要每 5 秒重试。
	CompactionCooldownSeconds = 60
	// compactionDegradeBackoffSeconds 连续降级的退避基数(秒):第 n 次连续降级后等待 base<<(n-1)。
	compactionDegradeBackoffSeconds = 120
	// CompactionMaxCooldownSeconds 退避上限(秒):再糟也不会超过该间隔。
	CompactionMaxCooldownSeconds = 3600
	// compactionThrottleMaxEntries 节流表容量上限(超出按最旧优先淘汰,内存有界)。
	compactionThrottleMaxEntries = 8192
)

// compactionThrottleEntry 一个会话的节流状态。
type compactionThrottleEntry struct {
	lastAttemptAt int64 // 上次尝试时刻(Unix 秒);0 表示尚未尝试
	degradedCount int   // 连续降级次数(成功的尝试会清零)
	failed        bool  // 上次尝试是否失败(摘要器报错/摘要为空等);失败不该享受"已推进"的短冷却
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
	defer t.mu.Unlock()

	entry := t.entries[sessionId]
	if entry == nil || entry.lastAttemptAt <= 0 {
		return true // 从未尝试过
	}
	return now-entry.lastAttemptAt >= cooldownSeconds(entry)
}

// RecordSuccess 记录一次成功的尝试(摘要已落盘);degraded 表示这次是降级覆盖。
func (t *CompactionThrottle) RecordSuccess(sessionId string, now int64, degraded bool) {
	t.record(sessionId, now, degraded, true)
}

// RecordFailure 记录一次"没有取得进展"的尝试(摘要器报错/摘要为空,或区间不需要压缩):
// 只更新时刻,保留连续降级计数 —— 失败既不该延长退避,也不该被当成"恢复了"。
// 目的:让"没活干"的会话也进入冷却,不要每轮重复一次整文件读回 + 协程。
func (t *CompactionThrottle) RecordFailure(sessionId string, now int64) {
	t.record(sessionId, now, false, false)
}

// Reset 清除某会话的节流状态(测试与运维用)。
//
// 压缩与合并共用这一张表但**分键**(见 mergeThrottleKey):两者必须一起清,
// 否则会出现"重置了却还在冷却"这种诡异状态。
func (t *CompactionThrottle) Reset(sessionId string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.entries, sessionId)
	delete(t.entries, mergeThrottleKey(sessionId))
}

// mergeThrottleKey 合并路径的节流键。
//
// 为什么与压缩**分键**:两者的失败模式与代价都不同 ——
// 压缩失败是"区间太短/降级后仍有损"(有降级兜底),合并失败是"无可安全合并的集合/合并后有损"
// (没有兜底,且摘要超预算是一个**持续**为真的状态)。
// 共用一个键会让"压缩刚推进"顺手抹掉合并的冷却,反之亦然。
//
// 键里带前缀不会与压缩键(sessionId 本身)碰撞:sessionId 经 NormalizeCompactId 规整,
// 只保留安全字符(不含 ':')。
func mergeThrottleKey(sessionId string) string {
	return "merge:" + strings.TrimSpace(sessionId)
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
	if _, ok := t.entries[sessionId]; !ok {
		t.evictLocked(now)
	}

	entry := t.entries[sessionId]
	if entry == nil {
		entry = &compactionThrottleEntry{}
		t.entries[sessionId] = entry
	}
	entry.lastAttemptAt = now
	// 一次成功就把"失败"状态清掉;失败则标记,以便用更长的冷却避免每 5 秒重试同一个报错。
	entry.failed = !countDegraded

	if !countDegraded {
		return
	}
	if degraded {
		entry.degradedCount++
	} else {
		entry.degradedCount = 0
	}
}

// evictLocked 在容量已满时腾出空间(调用方必须已持锁)。
//
// 先删过期条目(冷却早已结束,状态没有保留价值);仍然满则按"最久未尝试"淘汰最旧的一批。
// **不能整体清空**:那会让所有会话的冷却同时失效,可能出现一次同步化的压缩调用尖峰。
func (t *CompactionThrottle) evictLocked(now int64) {
	if len(t.entries) < compactionThrottleMaxEntries {
		return
	}

	expireBefore := now - CompactionMaxCooldownSeconds
	for key, entry := range t.entries {
		if entry.lastAttemptAt < expireBefore {
			delete(t.entries, key)
		}
	}
	if len(t.entries) < compactionThrottleMaxEntries {
		return
	}

	type agedEntry struct {
		key string
		at  int64
	}
	aged := make([]agedEntry, 0, len(t.entries))
	for key, entry := range t.entries {
		aged = append(aged, agedEntry{key: key, at: entry.lastAttemptAt})
	}
	sort.Slice(aged, func(i, j int) bool { return aged[i].at < aged[j].at })

	free := len(aged)/8 + 1 // 一次腾出 1/8,摊还成本(避免每次插入都全表扫描)
	for i := 0; i < free && i < len(aged); i++ {
		delete(t.entries, aged[i].key)
	}
}

// cooldownSeconds 按上次尝试的性质计算冷却时长:
//   - 失败:至少 CompactionCooldownSeconds(上游异常时不要每几秒重试);
//   - 降级:120s 起 2 倍退避并封顶(抑制降级抖动);
//   - 已推进(成功且未降级):很短,让积压能连续排空。
func cooldownSeconds(entry *compactionThrottleEntry) int64 {
	if entry == nil {
		return 0
	}

	backoff := degradeBackoffSeconds(entry.degradedCount)
	if entry.failed {
		if backoff > CompactionCooldownSeconds {
			return backoff
		}
		return CompactionCooldownSeconds
	}
	if backoff > 0 {
		return backoff
	}
	return CompactionProgressCooldownSeconds
}

// degradeBackoffSeconds 连续降级的退避时长;未降级返回 0。
func degradeBackoffSeconds(degradedCount int) int64 {
	if degradedCount <= 0 {
		return 0
	}

	backoff := int64(compactionDegradeBackoffSeconds)
	for i := 1; i < degradedCount && backoff < CompactionMaxCooldownSeconds; i++ {
		backoff *= 2
	}
	if backoff > CompactionMaxCooldownSeconds {
		backoff = CompactionMaxCooldownSeconds
	}
	return backoff
}
