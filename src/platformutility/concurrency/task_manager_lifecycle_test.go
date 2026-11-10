/**
* TaskManager 终态语义测试:Close / Drain / CancelScope / CancelDaemon。
*
* 核心约定:每个用例都用 NewTaskManager 创建**独立实例**,通过 SwapTaskManager
* 注入后执行终态操作,结束后关闭并还原。绝不对进程级单例调用这些方法。
**/

package concurrency_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"my.favorite/platformutility/apperr"
	"my.favorite/platformutility/concurrency"
)

// isolatedManager 创建一个与全局单例完全隔离的 Manager,并在用例结束时
// 关闭 + 还原全局指针。
func isolatedManager(t *testing.T, cfg concurrency.TaskConfig) *concurrency.TaskManager {
	t.Helper()

	m := concurrency.NewTaskManager(cfg)
	restore := concurrency.SwapTaskManager(m)

	t.Cleanup(func() {
		_ = m.Close(nil) // 幂等:已 Close 过也会立刻返回
		restore()
	})

	return m
}

// waitFor 轮询等待条件成立,超时即失败。
func waitFor(t *testing.T, d time.Duration, cond func() bool, msg string) {
	t.Helper()

	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(2 * time.Millisecond)
	}

	t.Fatalf("等待超时: %s", msg)
}

// TestClose_DrainsAllAndRejectsNew 验证 Close 的完整语义:
// 排空在飞任务 → 置 draining → 之后所有提交被拒。
func TestClose_DrainsAllAndRejectsNew(t *testing.T) {
	m := isolatedManager(t, concurrency.TaskConfig{
		MaxInflight:         32,
		DefaultPerNameLimit: 8,
		DrainTimeout:        2,
	})

	const n = 8

	var done atomic.Int64

	for i := 0; i < n; i++ {
		if err := m.Go(context.Background(), concurrency.RoutineSpec{
			Name: "close-task",
			Fn: func(ctx context.Context) error {
				time.Sleep(5 * time.Millisecond)
				done.Add(1)

				return nil
			},
		}); err != nil {
			t.Fatalf("Go 失败: %v", err)
		}
	}

	if err := m.Close(nil); err != nil {
		t.Fatalf("Close 应成功排空, got %v", err)
	}

	st := m.AllRoutineStats()
	if st.Inflight != 0 {
		t.Fatalf("Close 后不应有在飞任务, Inflight=%d", st.Inflight)
	}

	if done.Load() != n {
		t.Fatalf("Close 必须等待任务结束, done=%d want=%d", done.Load(), n)
	}

	if st.Completed != n {
		t.Fatalf("Completed=%d want=%d", st.Completed, n)
	}

	// 终态:此后再提交必须被拒绝
	err := m.Go(context.Background(), concurrency.RoutineSpec{
		Name: "after-close",
		Fn:   func(context.Context) error { return nil },
	})
	if err == nil {
		t.Fatal("Close 之后 Go 必须被拒绝")
	}

	if kind := apperr.Of(err); kind != concurrency.ErrTaskManagerClosed {
		t.Fatalf("错误类型不符: got %v", kind)
	}

	if !m.IsDraining() {
		t.Fatal("IsDraining 应为 true")
	}

	if m.AllRoutineStats().Rejected == 0 {
		t.Fatal("被拒绝的任务应计入 Rejected")
	}
}

// TestDrain_TimeoutReportsStillRunning 验证 Drain 超时:
// 返回 *DrainTimeoutError,并带回在飞任务快照(名称/scope/真实耗时)。
func TestDrain_TimeoutReportsStillRunning(t *testing.T) {
	m := isolatedManager(t, concurrency.TaskConfig{
		MaxInflight:         8,
		DefaultPerNameLimit: 4,
		DrainTimeout:        1, // 单位:秒
	})

	release := make(chan struct{})
	started := make(chan struct{})

	if err := m.Go(context.Background(), concurrency.RoutineSpec{
		Name: "stuck",
		Fn: func(ctx context.Context) error {
			close(started)
			<-release // 故意不响应 ctx:模拟不可中断的任务

			return nil
		},
	}); err != nil {
		t.Fatalf("Go 失败: %v", err)
	}

	<-started

	begin := time.Now()
	err := m.Drain(nil)
	elapsed := time.Since(begin)

	if err == nil {
		t.Fatal("Drain 应超时返回错误")
	}

	var derr *concurrency.DrainTimeoutError
	if !errors.As(err, &derr) {
		t.Fatalf("错误类型应为 *DrainTimeoutError, got %T: %v", err, err)
	}

	if !errors.Is(err, concurrency.ErrDrainTimeout) {
		t.Fatalf("errors.Is(ErrDrainTimeout) 应为 true")
	}

	if derr.Timeout != time.Second {
		t.Fatalf("Timeout=%s want=1s", derr.Timeout)
	}

	if elapsed < 900*time.Millisecond {
		t.Fatalf("应等到超时阈值才返回, elapsed=%s", elapsed)
	}

	if len(derr.Running) != 1 {
		t.Fatalf("在飞快照应为 1 条, got %d", len(derr.Running))
	}

	if derr.Running[0].Name != "stuck" || derr.Running[0].Scope != "quickScope" {
		t.Fatalf("快照内容不符: %+v", derr.Running[0])
	}

	if derr.Running[0].Elapsed <= 0 {
		t.Fatalf("RunningTask.Elapsed 未回填(需修复 snapshotRunning): %+v", derr.Running[0])
	}

	close(release)
	waitFor(t, 2*time.Second, func() bool {
		return m.AllRoutineStats().Inflight == 0
	}, "stuck 任务应结束")
}

// TestCancelDaemon_StopsDaemonButKeepsScopesAlive 验证 daemon 与 scope 的隔离:
// CancelDaemon 只杀 daemon;daemon 名不去重释放;请求级 scope 不受影响。
func TestCancelDaemon_StopsDaemonButKeepsScopesAlive(t *testing.T) {
	m := isolatedManager(t, concurrency.TaskConfig{
		MaxInflight:         8,
		DefaultPerNameLimit: 4,
		DrainTimeout:        1,
	})

	daemonStopped := make(chan struct{})

	if err := m.Daemon(concurrency.RoutineSpec{
		Name: "heartbeat",
		Fn: func(ctx context.Context) error {
			<-ctx.Done()
			close(daemonStopped)

			return nil
		},
	}); err != nil {
		t.Fatalf("Daemon 失败: %v", err)
	}

	m.CancelDaemon()

	select {
	case <-daemonStopped:
	case <-time.After(2 * time.Second):
		t.Fatal("CancelDaemon 后 daemon 未退出")
	}

	// 去重表不释放:同名 daemon 无法再注册 —— 这正说明它是终态操作
	err := m.Daemon(concurrency.RoutineSpec{
		Name: "heartbeat",
		Fn:   func(context.Context) error { return nil },
	})
	if err == nil {
		t.Fatal("同名 daemon 应被判为重复注册")
	}

	if kind := apperr.Of(err); kind != concurrency.ErrTaskDuplicatedDaemon {
		t.Fatalf("错误类型不符: got %v", kind)
	}

	// CancelDaemon 不影响请求级 scope(scopeCtx 仍然有效)
	ran := make(chan struct{})

	if err := m.Go(context.Background(), concurrency.RoutineSpec{
		Name: "after-cancel-daemon",
		Fn: func(ctx context.Context) error {
			close(ran)

			return nil
		},
	}); err != nil {
		t.Fatalf("CancelDaemon 后请求级任务仍应可提交: %v", err)
	}

	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("CancelDaemon 后请求级任务未执行")
	}
}

// TestCancelScope_CancelsDerivedScopes 验证 CancelScope 的效果:
// 已存在的 scope 与其在飞任务被取消;之后新建的 scope 拿到的是"已取消"的 ctx。
func TestCancelScope_CancelsDerivedScopes(t *testing.T) {
	m := isolatedManager(t, concurrency.TaskConfig{
		MaxInflight:         8,
		DefaultPerNameLimit: 4,
		DrainTimeout:        1,
	})

	scope := m.NewScope("req-1", 2)
	canceled := make(chan struct{})

	if err := scope.Go(context.Background(), concurrency.RoutineSpec{
		Name: "long",
		Fn: func(ctx context.Context) error {
			<-ctx.Done()
			close(canceled)

			return ctx.Err()
		},
	}); err != nil {
		t.Fatalf("scope.Go 失败: %v", err)
	}

	// 等任务真正起来再取消,验证的才是"打断在飞任务"
	waitFor(t, time.Second, func() bool {
		return m.AllRoutineStats().Inflight == 1
	}, "long 任务应已启动")

	m.CancelScope()

	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("CancelScope 后在飞任务未被取消")
	}

	// Scope.Wait 是"可重复创建、用完即弃"的安全收尾方式
	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := scope.Wait(waitCtx); err != nil {
		t.Fatalf("scope.Wait 应正常结束, got %v", err)
	}

	// CancelScope 不置 draining:新建 scope 仍可提交,但其 ctx 已随 scopeCtx 取消
	late := m.NewScope("req-2", 1)
	sawCanceled := make(chan bool, 1)

	if err := late.Go(context.Background(), concurrency.RoutineSpec{
		Name: "late",
		Fn: func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				sawCanceled <- true
			case <-time.After(time.Second):
				sawCanceled <- false
			}

			return nil
		},
	}); err != nil {
		t.Fatalf("late.Go 失败: %v", err)
	}

	select {
	case got := <-sawCanceled:
		if !got {
			t.Fatal("CancelScope 之后新建 scope 的 ctx 应已是取消状态")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("late 任务未执行")
	}
}

// TestClose_IsRepeatableAndDoesNotPanic 验证重复调用终态函数是安全的(幂等)。
func TestClose_IsRepeatableAndDoesNotPanic(t *testing.T) {
	m := isolatedManager(t, concurrency.TaskConfig{
		MaxInflight:         4,
		DefaultPerNameLimit: 2,
		DrainTimeout:        1,
	})

	if err := m.Close(nil); err != nil {
		t.Fatalf("首次 Close 失败: %v", err)
	}

	if err := m.Close(nil); err != nil {
		t.Fatalf("重复 Close 应立刻返回 nil, got %v", err)
	}

	// 底层是 context.CancelFunc,天然幂等
	m.CancelScope()
	m.CancelDaemon()
	m.CancelScope()
	m.CancelDaemon()
}

// TestSwapTaskManager_RestoresGlobal 验证注入/还原机制本身。
func TestSwapTaskManager_RestoresGlobal(t *testing.T) {
	before := concurrency.GetTaskManager()

	if before != concurrency.DefaultTaskManager() {
		t.Fatal("用例开始时全局应指向默认实例")
	}

	m := concurrency.NewTaskManager(concurrency.TaskConfig{})
	restore := concurrency.SwapTaskManager(m)

	if concurrency.GetTaskManager() != m {
		t.Fatal("Swap 之后 GetTaskManager 应返回注入实例")
	}

	restore()

	if concurrency.GetTaskManager() != before {
		t.Fatal("restore 之后应回到原实例")
	}

	// 幂等:重复 restore 不应把实例改回去
	restore()

	if concurrency.GetTaskManager() != before {
		t.Fatal("restore 应幂等")
	}

	_ = m.Close(nil) // 收尾:该实例不再被使用
}
