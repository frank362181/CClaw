/**
* 请求任务的作用域
*
* 与TaskManager的分工:
* - Manager 管理 "进程级":daemon、全局名额、Drain全部任务;
* - scope 管“请求级”:一次turn/一次扇出的等待与限额,不能污染全局drain
**/

package concurrency

import (
	"context"
	"sync"
)

type taskScope struct {
	taskMgr *TaskManager

	name  string
	limit int
	sem   chan struct{} // per-name 限额;nil==不限

	ctx    context.Context
	cancel context.CancelFunc
	daemon bool
	wg     sync.WaitGroup

	lock     sync.Mutex
	draining bool // wait之后置位:拒绝新任务
	closed   bool
}

func (s *taskScope) Name() string {
	return s.name
}

func (s *taskScope) Go(ctx context.Context, spec RoutineSpec) error {
	return s.taskMgr.dispatch(s, ctx, spec, true)
}

func (s *taskScope) TryGo(ctx context.Context, spec RoutineSpec) error {
	return s.taskMgr.dispatch(s, ctx, spec, false)
}

func (s *taskScope) Wait(ctx context.Context) error {
	s.lock.Lock()
	s.draining = true
	s.lock.Unlock()

	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), s.taskMgr.taskConfig.drainTimeout())
		defer cancel()
	}

	done := make(chan struct{})
	go func() { // 有界:任务全部结束时必须退出
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return &DrainTimeoutError{
			Timeout: s.taskMgr.taskConfig.drainTimeout(),
			Scope:   s.name,
			Running: s.taskMgr.snapshotRunning(s.name),
		}
	}
}

// Cancel 取消本scope,不等待
func (s *taskScope) Cancel() {
	s.lock.Lock()
	s.closed = true
	s.lock.Unlock()

	if s.cancel != nil {
		s.cancel()
	}

	// 从manager的活跃scope表中摘除(避免 Stats().Scopes长跑不降)
	if s.taskMgr != nil {
		// 删除一个scope
		s.taskMgr.DeleteScope(s)
	}
}

// Stats 返回一个快照
func (s *taskScope) Stats() TaskStats {
	if s.taskMgr == nil {
		return TaskStats{ByTask: make(map[string]TaskStat)}
	}

	return s.taskMgr.RoutineStats(s.name)
}

// 在scope锁内完成"是否可接纳"的判断与 wg.Add.
// 保证与Wait的draining置位严格互斥,不会漏等任务
func (s *taskScope) addIfOpen() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.closed || s.draining {
		return ErrTaskManagerClosed.Newf("scope %s has been closed or is draining", s.name)
	}

	s.wg.Add(1)
	return nil
}
