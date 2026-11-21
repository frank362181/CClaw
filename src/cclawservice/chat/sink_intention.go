package chat

import (
	"fmt"
	"sync/atomic"

	"my.favorite/cclawservice/chat/plan_mode"
	"my.favorite/cclawservice/common"
)

type SinkIntention struct {
	sink common.ISink

	total int
	done  atomic.Int64
}

var _ plan_mode.IPlanEventSink = (*SinkIntention)(nil)

func NewSinkIntention(sink common.ISink, total int) *SinkIntention {
	if sink == nil {
		sink = common.NopSink{}
	}

	return &SinkIntention{
		sink:  sink,
		total: total,
	}
}

func (sink *SinkIntention) OnTask(task plan_mode.PlanTask, state plan_mode.PlanTaskState) {
	label := GetTaskLabel(task)

	switch state.Status {
	case plan_mode.StatusRunning:
		sink.sink.OnStatus("plan.running:", label+"执行中...")
	case plan_mode.StatusSucceeded:
		sink.sink.OnStatus("plan done", fmt.Sprintf("%s 完成 (%d / %d)", label, sink.done.Add(1), sink.total))
	case plan_mode.StatusBlocked, plan_mode.StatusSkipped, plan_mode.StatusCanceled:
		sink.sink.OnStatus("plan skipped", label+"跳过")
	case plan_mode.StatusFailed, plan_mode.StatusTimeout:
		sink.sink.OnNotice(common.ChatNoticeError, fmt.Sprintf("%s 失败:%s", label, state.Err))
	}
}

func (sink *SinkIntention) OnGate(task plan_mode.PlanTask, gaps []plan_mode.Gap) {
	for _, gap := range gaps {
		code := common.ChatNoticeIncomplete
		if gap.Severity == plan_mode.SeverityFail {
			code = common.ChatNoticeError
		}

		sink.sink.OnNotice(code, fmt.Sprintf("校验 %s:%s", gap.Code, gap.Detail))
	}
}
