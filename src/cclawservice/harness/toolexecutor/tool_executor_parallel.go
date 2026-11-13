package toolexecutor

import (
	"context"
	"fmt"
	"strings"
	"sync"

	cCommon "my.favorite/cclawservice/common"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/concurrency"
	"my.favorite/platformutility/logging"
)

// UI 刷新事件缓冲大小,lines 的大小
// 太小 -> 事件频繁丢弃(UI刷新落后);太大->用户断链后残留更多无用事件
const defaultProgressBuffer = 16

// 单轮工具调用扇出的最大上限:限制单轮“同时开跑的工具数”
// 模型一次8个调用是常态
const maxToolFanout = 12

// parallelExecutor 流式接口下,同一轮的多个工具调用并行执行并把进度汇集到一个 sink
type parallelExecutor struct {
	core executorCore

	buf int //允许覆盖
}

func NewRoundExecutorParallel(runner IScriptRunner) IToolRoundExecutor {
	if runner == nil {
		panic("function calling: parallel executor requires a non-nil IToolScriptRunner")
	}
	return &parallelExecutor{
		core: executorCore{
			runner: runner,
		},
		buf: defaultProgressBuffer,
	}
}

func (pe *parallelExecutor) RunAll(ctx context.Context, env ToolEnv, invocations []ToolInvocation,
	sink ToolEventSink, ledger ToolLedger) []ToolOutcome {

	if len(invocations) == 0 {
		return nil
	}

	scopeName := strings.TrimSpace(env.ScopeName)
	if strings.TrimSpace(scopeName) == pCommon.BlankString {
		scopeName = "tool-turn"
	}

	// 等待事件,必须不同的scope,否则会与工具任务同一个scope,wait会永远不结束
	timing := GetToolRoundTiming()
	roundStart := pCommon.GetDatetimeUtilityInstance().GetNow()

	eventScope := concurrency.GetTaskManager().NewScope(scopeName+":events", 0)
	defer eventScope.Cancel()

	toolScope := concurrency.GetTaskManager().NewScope(scopeName+":tools", maxToolFanout)
	defer toolScope.Cancel()

	buf := pe.buf
	if buf <= 0 {
		buf = defaultProgressBuffer
	}

	lines := make(chan ToolEvent, buf)

	// 1.事件转发,写 SSE 的地方
	if err := eventScope.Go(ctx, concurrency.RoutineSpec{
		Name: "tool-event-forward",
		Fn: func(_ context.Context) error {
			for event := range lines {
				pe.deliver(sink, event)
			}
			return nil
		},
	}); err != nil {
		// 只有Manager已经关闭才会执行到这里,此时不能丢弃结果,退化为串行执行
		close(lines)
		logging.LogWarnF("tool executor: %s event forwarder unavailable(%s), fallback to serial", scopeName, err.Error())

		return (&serialExecutor{core: pe.core}).RunAll(ctx, env, invocations, sink, ledger)
	}

	// 按index 写回,保证返回顺序==模型声明顺序
	var outMu sync.Mutex
	out := make([]ToolOutcome, len(invocations))
	dispatched := make([]bool, len(invocations))
	for i, invocation := range invocations {
		if !send(ctx, lines, ToolEvent{Phase: cCommon.ChatToolEventRunning, Seq: invocation.Seq, Name: invocation.Name()}) {
			break
		}
		dispatched[i] = true

		idx, inv := i, invocation

		// 每个工具一个托管任务
		if err := toolScope.Go(ctx, concurrency.RoutineSpec{
			Name: "tool-" + inv.Name(),
			Fn: func(runCtx context.Context) error {
				pe.runOne(runCtx, env, inv, ledger, lines, func(o ToolOutcome) {
					outMu.Lock()
					out[idx] = o
					outMu.Unlock()
				})
				return nil
			},
		}); err != nil {
			// goroutine名额不可得且ctx已经结束,或manager正在关停
			dispatched[idx] = false
			logging.LogWarnF("tool executor: %s dispatch tool %s(seq=%d) failed: %s",
				scopeName, inv.Name(), inv.Seq, err.Error())
			break
		}
	}

	// 收敛:先等工具任务,再关通道装转发协程退出,最后等它把残留事件写完
	if err := waitScope(ctx, toolScope, timing); err != nil {
		logging.LogWarnF("tool executor: %s tool tasks not drained within %s: %s",
			scopeName, timing.RoundTimeout, err.Error())
	}

	close(lines)

	// 收敛事件转发
	if err := waitScope(ctx, eventScope, timing); err != nil {
		logging.LogWarnF("tool executor: %s event forwarder not drained within %s: %s",
			scopeName, timing.RoundTimeout, err.Error())
	}

	// 收敛:按顺序保留真正派发
	return normalizeOutcomes(invocations, dispatched, out, &outMu, roundStart)
}

// 执行一个工具,并保证"无论怎么都留下一条自洽的outcome"
func (pe *parallelExecutor) runOne(ctx context.Context, env ToolEnv, invocation ToolInvocation, ledger ToolLedger, lines chan<- ToolEvent, assign func(outcome ToolOutcome)) {
	hb := startHeartbeat(ctx, lines, invocation.Seq, invocation.Name(), pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp)
	defer hb.Stop()

	defer func() {
		if r := recover(); r != nil {
			logging.LogWarnF("tool executor: panic around tool %s(seq=%d):%v", invocation.Name(), invocation.Seq, r)
			assign(ToolOutcome{
				Seq:    invocation.Seq,
				CallID: invocation.ID(),
				Name:   invocation.Name(),
				Arg:    invocation.Args(),
				Reason: fmt.Errorf("panic around tool %s(seq=%d):%v", invocation.Name(), invocation.Seq, r),
				RespAt: pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp,
			})
		}
	}()

	assign(pe.core.executeOne(ctx, env, invocation, ledger, func(line string) {
		hb.Touch() // 有输出 --> 重置静默计时
		if pCommon.LooksLikeJson(line) {
			return // 结构结果不输出
		}
		hb.Emit(line)
	}))
}

func (pe *parallelExecutor) deliver(sink ToolEventSink, event ToolEvent) {
	defer func() {
		if r := recover(); r != nil {
			logging.LogWarnF("tool executor: sink panic on event phase=%s seq=%d: %v",
				event.Phase, event.Seq, r)
		}
	}()

	sink.OnTool(event)
}

// send 带ctx的阻塞投递
func send(ctx context.Context, ch chan<- ToolEvent, evt ToolEvent) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
		}
	}()
	select {
	case ch <- evt:
		return true
	case <-ctx.Done():
		return false
	}
}
