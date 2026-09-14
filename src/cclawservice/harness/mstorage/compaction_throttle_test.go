package mstorage

import (
	"strconv"
	"testing"
)

// newTestThrottle 独立的节流表(测试辅助):不使用全局单例,避免用例间相互影响。
func newTestThrottle() *CompactionThrottle {
	return &CompactionThrottle{entries: make(map[string]*compactionThrottleEntry)}
}

// TestCompactionThrottleEvictsOldestInsteadOfResettingAll 回归(D2):容量满时只淘汰最旧的一批,
// **不得整体清空** —— 整体清空会让所有会话的冷却同时失效,可能出现一次压缩调用尖峰。
func TestCompactionThrottleEvictsOldestInsteadOfResettingAll(t *testing.T) {
	th := newTestThrottle()
	base := int64(5_000_000)

	for i := 0; i < compactionThrottleMaxEntries; i++ {
		th.RecordSuccess("sess-"+strconv.Itoa(i), base+int64(i), false)
	}
	if len(th.entries) != compactionThrottleMaxEntries {
		t.Fatalf("应恰好填满容量: %d", len(th.entries))
	}

	now := base + int64(compactionThrottleMaxEntries)
	th.RecordSuccess("sess-new", now, false)

	if len(th.entries) > compactionThrottleMaxEntries {
		t.Errorf("淘汰后不应超过容量: %d", len(th.entries))
	}
	if th.entries["sess-0"] != nil {
		t.Errorf("最旧的会话应被淘汰")
	}
	if th.entries["sess-new"] == nil {
		t.Errorf("刚插入的会话不应被淘汰")
	}

	newest := "sess-" + strconv.Itoa(compactionThrottleMaxEntries-1)
	if th.entries[newest] == nil {
		t.Fatalf("最新的会话不应被淘汰")
	}
	// 关键:未被淘汰的会话仍在冷却中(整体清空会让这里变成"可压缩")
	if th.Allow(newest, now) {
		t.Errorf("未被淘汰的会话仍应处于冷却中(说明发生了整体清空)")
	}
}

// TestCompactionThrottleEvictsExpiredFirst 容量满时优先清理**已过期**的条目
// (冷却早已结束,状态没有保留价值),尽量不触碰仍在冷却中的会话。
func TestCompactionThrottleEvictsExpiredFirst(t *testing.T) {
	th := newTestThrottle()
	base := int64(9_000_000)
	fresh := base + CompactionMaxCooldownSeconds

	half := compactionThrottleMaxEntries / 2
	for i := 0; i < half; i++ {
		th.RecordSuccess("expired-"+strconv.Itoa(i), base-1, false) // 早已过期
	}
	for i := 0; i < compactionThrottleMaxEntries-half; i++ {
		th.RecordSuccess("fresh-"+strconv.Itoa(i), fresh, false)
	}

	th.RecordSuccess("newcomer", fresh, false)

	for i := 0; i < half; i++ {
		if th.entries["expired-"+strconv.Itoa(i)] != nil {
			t.Fatalf("过期条目应被优先清理: expired-%d", i)
		}
	}
	if th.entries["newcomer"] == nil {
		t.Errorf("新条目不应被淘汰")
	}
	for i := 0; i < 10; i++ {
		if th.entries["fresh-"+strconv.Itoa(i)] == nil {
			t.Errorf("未过期条目不应被清理: fresh-%d", i)
		}
	}
}

// TestCompactionThrottleCooldown 已推进(成功且未降级)后的冷却很短,便于把积压连续排空;
// 失败则用更长的冷却,避免每几秒重试同一个报错。
func TestCompactionThrottleProgressAndFailureCooldown(t *testing.T) {
	th := newTestThrottle()
	const sid = "throttle-cooldown"
	const now = int64(1_000_000)

	if ok := th.Allow(sid, now); !ok {
		t.Fatal("从未尝试过时应允许压缩")
	}

	th.RecordSuccess(sid, now, false)
	if ok := th.Allow(sid, now+CompactionProgressCooldownSeconds-1); ok {
		t.Errorf("推进后的短冷却期内不应允许压缩")
	}
	if ok := th.Allow(sid, now+CompactionProgressCooldownSeconds); !ok {
		t.Errorf("短冷却结束后应允许压缩(积压需要连续排空)")
	}

	// 失败:用基础的 60s 冷却,而不是 5s(避免上游异常时高频重试)
	const failSid = "throttle-failure"
	th.RecordFailure(failSid, now)
	if ok := th.Allow(failSid, now+CompactionProgressCooldownSeconds); ok {
		t.Errorf("失败后不应只等 %ds 就重试", CompactionProgressCooldownSeconds)
	}
	if ok := th.Allow(failSid, now+CompactionCooldownSeconds); !ok {
		t.Errorf("失败后 %ds 应解禁", CompactionCooldownSeconds)
	}

	// 失败后一旦成功,立即回到短冷却
	th.RecordSuccess(failSid, now+CompactionCooldownSeconds, false)
	if ok := th.Allow(failSid, now+CompactionCooldownSeconds+CompactionProgressCooldownSeconds); !ok {
		t.Errorf("成功后应恢复短冷却")
	}

	// 不同会话互不影响
	if ok := th.Allow("throttle-other", now); !ok {
		t.Errorf("其它会话不应受该会话冷却影响")
	}
}

// TestCompactionThrottleDegradeBackoff 连续降级按 2 倍退避(这正是"降级抖动"的抑制点)。
func TestCompactionThrottleDegradeBackoff(t *testing.T) {
	th := newTestThrottle()
	const sid = "throttle-degrade"
	now := int64(2_000_000)

	want := []int64{
		compactionDegradeBackoffSeconds,
		compactionDegradeBackoffSeconds * 2,
		compactionDegradeBackoffSeconds * 4,
	}
	for i, seconds := range want {
		th.RecordSuccess(sid, now, true) // 本次为降级
		if ok := th.Allow(sid, now+seconds-1); ok {
			t.Errorf("第 %d 次连续降级后冷却应为 %ds", i+1, seconds)
		}
		if ok := th.Allow(sid, now+seconds); !ok {
			t.Errorf("第 %d 次连续降级后 %ds 应解禁", i+1, seconds)
		}
	}
}

// TestCompactionThrottleBackoffCap 退避必须封顶,不会无限增长。
func TestCompactionThrottleBackoffCap(t *testing.T) {
	if got := cooldownSeconds(nil); got != 0 {
		t.Errorf("无记录时应为 0,实际 %d", got)
	}
	if got := cooldownSeconds(&compactionThrottleEntry{}); got != CompactionProgressCooldownSeconds {
		t.Errorf("已推进时应为短冷却 %d,实际 %d", CompactionProgressCooldownSeconds, got)
	}
	if got := cooldownSeconds(&compactionThrottleEntry{failed: true}); got != CompactionCooldownSeconds {
		t.Errorf("失败时应为基础冷却 %d,实际 %d", CompactionCooldownSeconds, got)
	}
	first := cooldownSeconds(&compactionThrottleEntry{degradedCount: 1})
	if first != compactionDegradeBackoffSeconds {
		t.Errorf("首次降级退避应为 %d,实际 %d", compactionDegradeBackoffSeconds, first)
	}
	for d := 1; d <= 64; d++ {
		got := cooldownSeconds(&compactionThrottleEntry{degradedCount: d})
		if got > CompactionMaxCooldownSeconds {
			t.Fatalf("退避未封顶: degraded=%d cooldown=%d", d, got)
		}
		if got < compactionDegradeBackoffSeconds {
			t.Fatalf("退避不应低于基础冷却: degraded=%d cooldown=%d", d, got)
		}
	}
	if got := cooldownSeconds(&compactionThrottleEntry{degradedCount: 64}); got != CompactionMaxCooldownSeconds {
		t.Errorf("多次降级后应达到上限 %d,实际 %d", CompactionMaxCooldownSeconds, got)
	}
}

// TestCompactionThrottleSuccessResetsDegraded 一次正常(未降级)的压缩应清零降级计数,回到基础冷却。
func TestCompactionThrottleSuccessResetsDegraded(t *testing.T) {
	th := newTestThrottle()
	const sid = "throttle-reset-degraded"
	now := int64(3_000_000)

	th.RecordSuccess(sid, now, true)  // 降级 → 退避 120s
	th.RecordSuccess(sid, now, false) // 恢复 → 计数清零
	if ok := th.Allow(sid, now+CompactionCooldownSeconds); !ok {
		t.Errorf("计数清零后应按基础冷却解禁")
	}
}

// TestCompactionThrottleFailureKeepsDegradedCount 失败只更新时间,既不延长退避也不清零计数。
func TestCompactionThrottleFailureKeepsDegradedCount(t *testing.T) {
	th := newTestThrottle()
	const sid = "throttle-failure"
	now := int64(4_000_000)

	th.RecordSuccess(sid, now, true) // 降级 → 退避 120s
	th.RecordFailure(sid, now)       // 摘要器报错

	if ok := th.Allow(sid, now+CompactionCooldownSeconds); ok {
		t.Errorf("失败后仍应按降级退避冷却(而非基础冷却)")
	}
	if ok := th.Allow(sid, now+compactionDegradeBackoffSeconds); !ok {
		t.Errorf("退避结束后应解禁")
	}
}

// TestCompactionThrottleReset Reset 应清除该会话的节流状态。
func TestCompactionThrottleReset(t *testing.T) {
	th := newTestThrottle()
	const sid = "throttle-reset"
	now := int64(5_000_000)

	th.RecordSuccess(sid, now, true)
	if ok := th.Allow(sid, now); ok {
		t.Fatal("刚尝试过应被冷却")
	}
	th.Reset(sid)
	if ok := th.Allow(sid, now); !ok {
		t.Errorf("Reset 后应立即允许")
	}
}

// TestCompactionThrottleCapacityBounded 节流表容量有界:超出上限时整体重置,内存不随会话数无限增长。
func TestCompactionThrottleCapacityBounded(t *testing.T) {
	th := newTestThrottle()
	for i := 0; i < compactionThrottleMaxEntries+16; i++ {
		th.RecordSuccess("s-"+strconv.Itoa(i), int64(i), false)
	}
	if len(th.entries) > compactionThrottleMaxEntries {
		t.Errorf("节流表超出容量上限: %d", len(th.entries))
	}
}
