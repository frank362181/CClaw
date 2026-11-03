package concurrency

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"
)

type TaskManager struct {
	taskConfig TaskConfig

	lock     sync.RWMutex
	draining bool // Drain/Close 后,不再接受新任务

	quickScope *taskScope

	// 请求的根context:所有的scope都由它派生,取消时统一取消
	scopeCtx    context.Context
	scopeCancel context.CancelFunc

	// daemon 根:与调用方完全解耦,只有stop()时才能取消
	daemonCtx    context.Context
	daemonCancel context.CancelFunc

	// 全局名额;nil=不限制
	globalSemaphore chan struct{}

	scopes  map[*taskScope]struct{}
	daemons map[string]struct{} // daemon 去重名
	running map[int64]RunningTask
	nextId  atomic.Int64
	byTask  map[string]*TaskStat

	// 全局的task统计信息,使用TaskStats不需要额外定义个基本成员对象相同的struct
	totalTaskStat TaskStats

	wg sync.WaitGroup // 所有已启动的任务(scope任务 + daemon)
}

var taskManager = pCommon.GetSingleton(func() *TaskManager {
	rCtx, rCancel := context.WithCancel(context.Background())
	dCtx, dCancel := context.WithCancel(context.Background())

	taskManager := &TaskManager{
		scopeCtx:    rCtx,
		scopeCancel: rCancel,

		daemonCtx:    dCtx,
		daemonCancel: dCancel,

		scopes:  make(map[*taskScope]struct{}),
		daemons: make(map[string]struct{}),
		running: make(map[int64]RunningTask),
		byTask:  make(map[string]*TaskStat),
	}

	// 必须先对task manager的配置进行初始化
	taskManager.taskConfig = taskManager.taskConfig.defaultTaskConfig()

	if taskManager.taskConfig.MaxInflight > 0 {
		taskManager.globalSemaphore = make(chan struct{}, taskManager.taskConfig.MaxInflight)
	}

	// 即发即弃场景复用同一scope
	taskManager.quickScope = taskManager.newScope("quickScope", taskManager.taskConfig.DefaultPerNameLimit)

	return taskManager
}).Instance()

func GetTaskManager() IRoutineManager {
	return taskManager
}

func (tm *TaskManager) Go(ctx context.Context, routineSpec RoutineSpec) error {
	return tm.dispatch(tm.quickScope, ctx, routineSpec, true)
}

func (tm *TaskManager) TryGo(ctx context.Context, routineSpec RoutineSpec) error {
	return tm.dispatch(tm.quickScope, ctx, routineSpec, false)
}

func (tm *TaskManager) NewScope(scopeName string, limit int) IRoutineScope {
	scopeName = strings.TrimSpace(scopeName)
	if scopeName == pCommon.BlankString {
		scopeName = "scope" // default scope name
	}

	if limit <= 0 {
		limit = tm.taskConfig.DefaultPerNameLimit
	}

	scope := tm.newScope(scopeName, limit)

	tm.lock.Lock()
	defer tm.lock.Unlock()
	tm.scopes[scope] = struct{}{}

	return scope
}

// Daemon 注册进程级常驻任务
func (tm *TaskManager) Daemon(routineSpec RoutineSpec) error {
	if err := routineSpec.validate(); err != nil {
		tm.bumpRejected()
		return err
	}

	tm.lock.Lock()
	if tm.draining {
		tm.lock.Unlock()
		tm.bumpRejected()
		return ErrTaskManagerClosed.Newf("daemon:%s rejected,task manager is draining", routineSpec.Name)
	}

	if _, dup := tm.daemons[routineSpec.Name]; dup {
		tm.lock.Unlock()
		tm.bumpRejected()
		return ErrTaskDuplicatedDaemon.Newf("daemon:%s already registered", routineSpec.Name)
	}

	tm.daemons[routineSpec.Name] = struct{}{}
	tm.lock.Unlock()

	scope := tm.newDaemonScope(routineSpec.Name, routineSpec.Limit)
	if err := tm.dispatch(scope, scope.ctx, routineSpec, true); err != nil {
		tm.lock.Lock()
		delete(tm.daemons, routineSpec.Name) // 回滚,避免名称永久占用
		tm.lock.Unlock()
		return err
	}

	return nil
}

// CancelScope 取消即触即弃的routine,Daemon不受影响
func (tm *TaskManager) CancelScope() {
	tm.scopeCancel()
}

// CancelDaemon daemon的routine
func (tm *TaskManager) CancelDaemon() {
	tm.daemonCancel()
}

func (tm *TaskManager) IsDraining() bool {
	tm.lock.RLock()
	defer tm.lock.RUnlock()

	return tm.draining
}

func (tm *TaskManager) Close(ctx context.Context) error {
	tm.CancelScope()
	tm.CancelDaemon()

	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(tm.taskConfig.DrainTimeout)*time.Second)
		defer cancel()
	}

	return tm.Drain(ctx)
}

// Drain 等待所有启动的任务结束
func (tm *TaskManager) Drain(ctx context.Context) error {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), tm.taskConfig.drainTimeout())
		defer cancel()
	}

	// 先关闭再等待:保证wg.Add 不会与wg.Wait竞争(Go的wWaitGroup只在计数 > 0 时允许并发 Add)
	tm.lock.Lock()
	tm.draining = true
	tm.lock.Unlock()

	done := make(chan struct{})
	go func() { // 有界:任务全部结束时必退出
		tm.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return &DrainTimeoutError{
			Timeout: tm.taskConfig.drainTimeout(),
			Scope:   pCommon.BlankString,
			Running: tm.snapshotRunning(pCommon.BlankString),
		}
	}
}

// AllRoutineStats 获取所有Routine的统计信息,值拷贝
func (tm *TaskManager) AllRoutineStats() TaskStats {
	tm.lock.RLock()
	defer tm.lock.RUnlock()

	st := TaskStats{
		Inflight:    tm.totalTaskStat.Inflight,
		MaxInflight: tm.totalTaskStat.MaxInflight,
		Started:     tm.totalTaskStat.Started,
		Completed:   tm.totalTaskStat.Completed,
		Failed:      tm.totalTaskStat.Failed,
		Panicked:    tm.totalTaskStat.Panicked,
		Rejected:    tm.totalTaskStat.Rejected,
		Scopes:      len(tm.scopes),
		ByTask:      make(map[string]TaskStat, len(tm.byTask)),
	}

	for name, ts := range tm.byTask {
		st.ByTask[name] = *ts
	}

	return st
}

// RoutineStats 以scope视觉返回统计信息
// 历史累计started/rejected/completed无法按照scope进行拆分的,以AllRoutineStats为准
func (tm *TaskManager) RoutineStats(scopeName string) TaskStats {
	tm.lock.RLock()
	defer tm.lock.RUnlock()

	tss := TaskStats{ByTask: make(map[string]TaskStat)}

	for _, task := range tm.running {
		if task.Scope != scopeName {
			continue
		}

		ts := tss.ByTask[task.Name]
		ts.Inflight++
		tss.ByTask[task.Name] = ts

		tss.Inflight++
		if tss.Inflight > tss.MaxInflight {
			tss.MaxInflight = tss.Inflight
		}
	}

	if tss.Inflight > 0 {
		tss.Scopes = 1 //一个scope作用域
	}

	return tss
}

func (tm *TaskManager) DeleteScope(s *taskScope) {
	tm.lock.Lock()
	defer tm.lock.Unlock()

	delete(tm.scopes, s)
}

// ============================== private functions & methods =================================

// 请求级scope的统一构造点
func (tm *TaskManager) newScope(scopeName string, limit int) *taskScope {
	ctx, cancel := context.WithCancel(tm.scopeCtx)

	s := &taskScope{
		taskMgr: tm,
		name:    scopeName,
		limit:   limit,
		ctx:     ctx,
		cancel:  cancel,
	}
	if limit > 0 {
		s.sem = make(chan struct{}, limit)
	}

	return s
}

// daemon 级别作用域,便于按照daemon进行观察
func (tm *TaskManager) newDaemonScope(name string, limit int) *taskScope {
	s := &taskScope{
		taskMgr: tm,
		name:    "daemon:" + name,
		limit:   limit,
		ctx:     tm.daemonCtx,
		daemon:  true,
	}

	if limit > 0 {
		s.sem = make(chan struct{}, limit)
	}

	return s
}

// dispatch 创建 goroutine 入口
func (tm *TaskManager) dispatch(s *taskScope, callerCtx context.Context, routineSpec RoutineSpec, block bool) error {
	if err := routineSpec.validate(); err != nil {
		tm.bumpRejected()
		return err
	}

	if callerCtx == nil {
		callerCtx = context.Background()
	}

	// 1. 是否关闭检查
	tm.lock.Lock()
	draining := tm.draining
	if draining {
		tm.lock.Unlock()
		tm.bumpRejected()
		return ErrTaskManagerClosed.Newf("task %s rejected,task manager is closed or draining", routineSpec.Name)
	}
	tm.wg.Add(1)
	tm.lock.Unlock()

	// 2. scope接纳检查 与 wg.Add在同一把锁内完成
	// 否则会出现"Wait已经开始等待,wg.Add才发生"从而漏等任务
	if err := s.addIfOpen(); err != nil {
		tm.wg.Done()
		tm.bumpRejected()
		return err
	}

	// 1. 全局名额
	if err := acquire(callerCtx, tm.globalSemaphore, block); err != nil {
		s.wg.Done()
		tm.wg.Done()
		tm.bumpRejected()
		return ErrTaskNoCapacity.Newf("task %s rejected,scope=global", routineSpec.Name).WithCause(err)
	}

	// 2. scope名额
	if err := acquire(callerCtx, s.sem, block); err != nil {
		release(tm.globalSemaphore, routineSpec.Name)
		s.wg.Done()
		tm.wg.Done()
		tm.bumpRejected()
		return ErrTaskNoCapacity.Newf("task:=%s rejected,scope=%s", routineSpec.Name, s.name).WithCause(err)
	}

	// 3. 登记 + 起goroutine
	id := tm.nextId.Add(1)
	tm.lock.Lock()
	tm.running[id] = RunningTask{
		TaskId: id,
		Name:   routineSpec.Name,
		Scope:  s.name,
		Start:  pCommon.GetDatetimeUtilityInstance().GetNow(),
	}
	ts := tm.statisticsLocked(routineSpec.Name)
	ts.Started++
	ts.Inflight++
	if ts.Inflight > ts.MaxInflight {
		ts.MaxInflight = ts.Inflight
	}
	tm.totalTaskStat.Started++
	tm.totalTaskStat.Inflight++
	if tm.totalTaskStat.Inflight > tm.totalTaskStat.MaxInflight {
		tm.totalTaskStat.MaxInflight = tm.totalTaskStat.Inflight
	}
	tm.lock.Unlock()

	start := pCommon.GetDatetimeUtilityInstance().GetNow()
	var failed atomic.Bool
	go func() {
		// defer注册顺序
		defer s.wg.Done()
		defer tm.wg.Done()
		defer release(s.sem, routineSpec.Name)
		defer release(tm.globalSemaphore, routineSpec.Name)
		defer tm.onFinished(id, routineSpec.Name, &failed, start)
		defer tm.onHandlePanic(routineSpec)

		if s.daemon {
			if err := tm.runDaemon(s.ctx, routineSpec, start); err != nil {
				failed.Store(true)
			}
			return
		}

		// 请求内任务:调用方ctx与scope ctx合流,任一去取消都会取消任务
		runCtx, runCancel := context.WithCancel(callerCtx)
		stopWatch := context.AfterFunc(s.ctx, runCancel) // requires Go 1.21+
		defer func() {
			stopWatch()
			runCancel()
		}()

		if err := tm.execute(runCtx, routineSpec); err != nil {
			failed.Store(true) // 唯一记账点 onFinished
			logging.LogWarnF("task %s:returned error:%v", routineSpec.Name, err)
		}
	}()

	return nil
}

// Daemon的重启循环,ctx只由Stop取消
func (tm *TaskManager) runDaemon(ctx context.Context, routineSpec RoutineSpec, start time.Time) error {
	attempt := 0
	for {
		err := tm.execute(ctx, routineSpec)
		if ctx.Err() != nil || err == nil {
			return nil // 正常停止/正常结束
		}

		if !routineSpec.Restart.Errored || routineSpec.Restart.MaxRetries <= 0 {
			return err
		}

		attempt++
		if attempt > routineSpec.Restart.MaxRetries {
			return ErrTaskOverRetries.Newf("task: daemon %s gave up after %d restarted(s):%v", routineSpec.Name, attempt-1, err)
		}

		backoff := routineSpec.Restart.BackOff
		if backoff <= 0 {
			backoff = time.Second
		}
		wait := backoff * time.Duration(attempt)

		logging.LogWarnF("task:daemon %s restart #%d in %s (elapsed %s):%v", routineSpec.Name, attempt, wait,
			time.Since(start).Truncate(time.Millisecond), err)

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

// 在受控上下文中执行一次Fn,并把panic转成error
func (tm *TaskManager) execute(ctx context.Context, routineSpec RoutineSpec) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if routineSpec.Panic == CrashPanic {
				panic(r) // 交给进程兜底
			}

			tm.notePanic(routineSpec.Name, r, debug.Stack())
			err = fmt.Errorf("task %s panicked: %v", routineSpec.Name, r)
		}
	}()

	return routineSpec.Fn(ctx)
}

// 唯一出口:归还还在飞、inflight、耗时统计,并打慢任务告警
func (tm *TaskManager) onFinished(id int64, name string, failed *atomic.Bool, start time.Time) {
	tm.lock.Lock()
	rt, ok := tm.running[id]
	if ok {
		delete(tm.running, id)
	}

	ts := tm.statisticsLocked(name)
	if ts.Inflight > 0 {
		ts.Inflight--
	}

	if tm.totalTaskStat.Inflight > 0 {
		tm.totalTaskStat.Inflight--
	}

	if failed != nil && failed.Load() {
		ts.Failed++
		tm.totalTaskStat.Failed++
	} else {
		ts.Completed++
		tm.totalTaskStat.Completed++
	}

	var elapsed time.Duration
	if ok {
		elapsed = time.Since(start)
		ms := elapsed.Milliseconds()
		ts.TotalMs += ms
		if ms > ts.SlowestMs {
			ts.SlowestMs, ts.SlowestTaskId = ms, id
		}
	}

	slow := elapsed >= tm.taskConfig.slowThreshold()
	scopeName := rt.Scope
	tm.lock.Unlock()

	if slow {
		logging.LogWarnF("task: slow task %s/%s(id=%d) took %s", scopeName, name, id, elapsed.Truncate(time.Millisecond))
	}
}

// 兜底recover,对RecoverOnPanic的任务而言
// execute已经recover过,这里是第二道网
func (tm *TaskManager) onHandlePanic(routineSpec RoutineSpec) {
	if r := recover(); r != nil {
		if routineSpec.Panic == CrashPanic {
			panic(r)
		}
		tm.notePanic(routineSpec.Name, r, debug.Stack())
	}
}

// 创建或取出一个TaskStat对象
func (tm *TaskManager) statisticsLocked(scopeName string) *TaskStat {
	ts, ok := tm.byTask[scopeName]
	if !ok {
		ts = &TaskStat{}
		tm.byTask[scopeName] = ts
	}

	return ts
}

func (tm *TaskManager) bumpRejected() {
	tm.lock.Lock()
	defer tm.lock.Unlock()

	tm.totalTaskStat.Rejected++
}

func (tm *TaskManager) notePanic(name string, r any, stack []byte) {
	tm.lock.Lock()
	defer tm.lock.Unlock()

	if st, ok := tm.byTask[name]; ok {
		st.Panicked++
	}
	tm.totalTaskStat.Panicked++

	logging.LogWarnF("task statistics:panic in %s: %v\n %s", name, r, string(stack))
}

// 生成Drain 超时快照,scopeName为空表示全部
func (tm *TaskManager) snapshotRunning(scopeName string) []RunningTask {
	tm.lock.RLock()
	defer tm.lock.RUnlock()

	startAt := pCommon.GetDatetimeUtilityInstance().GetNow()
	out := make([]RunningTask, 0, len(tm.running))
	for id, rt := range tm.running {
		if scopeName != pCommon.BlankString && scopeName != rt.Scope {
			continue
		}

		out = append(out, RunningTask{
			TaskId:  id,
			Name:    rt.Name,
			Scope:   rt.Scope,
			Start:   startAt,
			Elapsed: time.Since(startAt),
		})
	}

	return out
}
