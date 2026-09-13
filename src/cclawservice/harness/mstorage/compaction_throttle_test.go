package mstorage

import (
	"strconv"
	"testing"
)

// newTestThrottle 独立的节流表(测试辅助):不使用全局单例,避免用例间相互影响。
func newTestThrottle() *CompactionThrottle {
	return &CompactionThrottle{entries: make(map[string]*compactionThrottleEntry)}
}

// TestCompactionThrottleCooldown 一次正常尝试后进入基础冷却期。
func TestCompactionThrottleCooldown(t *testing.T) {
	th := newTestThrottle()
	const sid = "throttle-cooldown"
	const now = int64(1_000_000)

	if ok := th.Allow(sid, now); !ok {
		t.Fatal("从未尝试过时应允许压缩")
	}

	th.RecordSuccess(sid, now, false)
	if ok := th.Allow(sid, now+CompactionCooldownSeconds-1); ok {
		t.Errorf("冷却期内不应允许压缩")
	}
	if ok := th.Allow(sid, now+CompactionCooldownSeconds); !ok {
		t.Errorf("冷却结束后应允许压缩")
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
	if got := cooldownSeconds(0); got != CompactionCooldownSeconds {
		t.Errorf("未降级时应为基础冷却 %d,实际 %d", CompactionCooldownSeconds, got)
	}
	if got := cooldownSeconds(1); got != compactionDegradeBackoffSeconds {
		t.Errorf("首次降级退避应为 %d,实际 %d", compactionDegradeBackoffSeconds, got)
	}
	for d := 1; d <= 64; d++ {
		got := cooldownSeconds(d)
		if got > CompactionMaxCooldownSeconds {
			t.Fatalf("退避未封顶: degraded=%d cooldown=%d", d, got)
		}
		if got < CompactionCooldownSeconds {
			t.Fatalf("退避不应低于基础冷却: degraded=%d cooldown=%d", d, got)
		}
	}
	if got := cooldownSeconds(64); got != CompactionMaxCooldownSeconds {
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
