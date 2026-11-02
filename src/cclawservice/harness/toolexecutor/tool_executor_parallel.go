package toolexecutor

import (
	"context"
	"sync"

	cCommon "my.favorite/cclawservice/common"
	pCommon "my.favorite/platformutility/common"
)

// UI 刷新事件缓冲大小,lines 的大小
// 太小 -> 事件频繁丢弃(UI刷新落后);太大->用户断链后残留更多无用事件
const defaultProgressBuffer = 16

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

	buf := pe.buf
	if buf <= 0 {
		buf = defaultProgressBuffer
	}

	lines := make(chan ToolEvent, buf)
	fwDone := make(chan struct{})
	go func() {
		defer close(fwDone)
		for event := range lines {
			sink.OnTool(event) // 唯一写 SSE的地方
		}
	}()

	// 按index写回,保证返回顺序 == 模型声明顺序
	out := make([]ToolOutcome, len(invocations))
	dispatched := make([]bool, len(invocations))

	var wg sync.WaitGroup

	for i, invocation := range invocations {
		// running必须在**派发前**发出:否则可能出现 "progress先到、running后到",前端卡片状态倒退的现象
		if !send(ctx, lines, ToolEvent{Phase: cCommon.ChatToolEventRunning, Seq: invocation.Seq, Name: invocation.Name()}) {
			break
		}
		dispatched[i] = true

		wg.Add(1)
		go func(i int, invocation ToolInvocation) {
			defer wg.Done()

			hb := startHeartbeat(ctx, lines, invocation.Seq, invocation.Name(), pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp)
			defer hb.Stop()

			out[i] = pe.core.executeOne(ctx, env, invocation, ledger, func(line string) {
				hb.Touch() //有输出 -> 重置静默计时
				if pCommon.LooksLikeJson(line) {
					return // 结构化结果不推给UI,不是给人看的
				}

				hb.Emit(line)
			})
		}(i, invocation)
	}

	wg.Wait()
	close(lines)
	<-fwDone //等转发goroutine把残留事件写完再返回(否则sink可能被后续代码并发访问)

	// 收敛:按声明顺序只保留真正派发过的
	res := make([]ToolOutcome, 0, len(invocations))
	for i := range out {
		if dispatched[i] {
			res = append(res, out[i])
		}
	}

	return res
}

// send 带ctx的阻塞投递
func send(ctx context.Context, ch chan<- ToolEvent, evt ToolEvent) bool {
	select {
	case ch <- evt:
		return true
	case <-ctx.Done():
		return false
	}
}
