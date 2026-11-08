package archtest

import (
	"context"
	"testing"
	"time"

	"my.favorite/platformutility/concurrency"
)

const archtestWaitTimeout = 5 * time.Second

// waitInflightBack 轮询等待全局在飞数回到 baseline。
func waitInflightBack(t *testing.T, m concurrency.IRoutineManager, baseline int64, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if m.AllRoutineStats().Inflight <= baseline {
			return
		}

		time.Sleep(2 * time.Millisecond)
	}

	t.Fatalf("inflight 未回到 baseline=%d: %+v", baseline, m.AllRoutineStats())
}

// TestScopeLeavesNoInflight 守护 I2:scope.Wait 返回后,本 scope 内没有任务在跑,
// 且全局在飞数回到调用前的水平(无残留 goroutine)。
//
// [为什么不 Drain] 进程级单例一旦 Drain 就永久 draining,会把同进程里后续的测试全部毒死。
// scope 的 Wait 只影响该 scope(它自己的 draining),既能验证收敛、又不污染单例 ——
// 这也是把"请求级收敛"和"进程级关停"分成两种作用的初衷。
func TestScopeLeavesNoInflight(t *testing.T) {
	m := concurrency.GetTaskManager()
	before := m.AllRoutineStats()

	const tasks = 20

	scope := m.NewScope("archtest:leak", 4)

	for i := 0; i < tasks; i++ {
		if err := scope.Go(context.Background(), concurrency.RoutineSpec{
			Name: "archtest-task",
			Fn: func(ctx context.Context) error {
				time.Sleep(2 * time.Millisecond)
				return nil
			},
		}); err != nil {
			scope.Cancel()
			t.Fatalf("scope.Go #%d: %v", i, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), archtestWaitTimeout)
	defer cancel()

	if err := scope.Wait(ctx); err != nil {
		scope.Cancel()
		t.Fatalf("scope.Wait: %v", err)
	}

	// I2(scope 版):Wait 返回后本 scope 内 inflight 必须为 0
	if st := scope.Stats(); st.Inflight != 0 {
		scope.Cancel()
		t.Fatalf("Wait 之后 scope 仍有在飞任务: inflight=%d", st.Inflight)
	}

	// 全局在飞数必须回到调用前水平(无泄漏)
	waitInflightBack(t, m, before.Inflight, archtestWaitTimeout)

	scope.Cancel() // 从 manager 的活跃 scope 表中摘除

	after := m.AllRoutineStats()
	if after.Inflight != before.Inflight {
		t.Fatalf("全局在飞数未回落到 baseline: before=%d after=%d", before.Inflight, after.Inflight)
	}
	if after.Started-before.Started < int64(tasks) {
		t.Fatalf("started 增量=%d, want >= %d(任务丢了?)", after.Started-before.Started, tasks)
	}
	if after.Failed != before.Failed || after.Panicked != before.Panicked {
		t.Fatalf("出现了 failed/panicked: before(fail=%d panic=%d) after(fail=%d panic=%d)",
			before.Failed, before.Panicked, after.Failed, after.Panicked)
	}
	if after.Scopes != before.Scopes {
		t.Fatalf("scope 未摘除(泄漏): before=%d after=%d", before.Scopes, after.Scopes)
	}
}

// TestPanicDoesNotEscape 守护 I1:任务内 panic 不逃逸到进程、只计一次、且 Manager 仍可用。
func TestPanicDoesNotEscape(t *testing.T) {
	m := concurrency.GetTaskManager()
	before := m.AllRoutineStats()

	scope := m.NewScope("archtest:panic", 2)

	if err := scope.Go(context.Background(), concurrency.RoutineSpec{
		Name: "archtest-panic",
		Fn:   func(ctx context.Context) error { panic("archtest boom") },
	}); err != nil {
		scope.Cancel()
		t.Fatalf("scope.Go: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), archtestWaitTimeout)
	defer cancel()

	// 能走到 Wait 返回,本身就说明 panic 没有掀翻进程
	if err := scope.Wait(ctx); err != nil {
		scope.Cancel()
		t.Fatalf("scope.Wait: %v", err)
	}

	scope.Cancel()

	after := m.AllRoutineStats()
	if got := after.Panicked - before.Panicked; got != 1 {
		t.Fatalf("panicked 增量=%d, want 1", got)
	}
	if got := after.Failed - before.Failed; got != 1 {
		t.Fatalf("failed 增量=%d, want 1(panic 应记一次失败)", got)
	}

	// panic 之后 Manager 仍可继续派发
	next := m.NewScope("archtest:after-panic", 1)

	var ran bool
	if err := next.Go(context.Background(), concurrency.RoutineSpec{
		Name: "archtest-after-panic",
		Fn:   func(ctx context.Context) error { ran = true; return nil },
	}); err != nil {
		next.Cancel()
		t.Fatalf("panic 之后 scope.Go 失败: %v", err)
	}

	if err := next.Wait(ctx); err != nil {
		next.Cancel()
		t.Fatalf("panic 之后 scope.Wait 失败: %v", err)
	}
	next.Cancel()

	if !ran {
		t.Fatal("panic 之后任务没有执行")
	}

	waitInflightBack(t, m, before.Inflight, archtestWaitTimeout)
}
