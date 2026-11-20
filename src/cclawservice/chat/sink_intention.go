package chat

import (
	"fmt"
	"sync/atomic"

	"my.favorite/cclawservice/chat/sop"
	"my.favorite/cclawservice/common"
)

type SinkIntention struct {
	sink  ISink
	total int
	done  atomic.Int64
}

var _ sop.IPlanEventSink = (*SinkIntention)(nil)

func NewSinkIntention(sink ISink, total int) *SinkIntention {
	if sink == nil {
		sink = NopSink{}
	}

	return &SinkIntention{
		sink:  sink,
		total: total,
	}
}

func (sink *SinkIntention) OnTask(task sop.PlanTask, state sop.PlanTaskState) {
	label := GetTaskLabel(task)

	switch state.Status {
	case sop.StatusRunning:
		sink.sink.OnStatus("plan.running:", label+"执行中...")
	case sop.StatusSucceeded:
		sink.sink.OnStatus("plan done", fmt.Sprintf("%s 完成 (%d / %d)", label, sink.done.Add(1), sink.total))
	case sop.StatusBlocked, sop.StatusSkipped, sop.StatusCanceled:
		sink.sink.OnStatus("plan skipped", label+"跳过")
	case sop.StatusFailed, sop.StatusTimeout:
		sink.sink.OnNotice(common.ChatNoticeError, fmt.Sprintf("%s 失败:%s", label, state.Err))
	}
}

func (sink *SinkIntention) OnGate(task sop.PlanTask, gaps []sop.Gap) {
	for _, gap := range gaps {
		code := common.ChatNoticeIncomplete
		if gap.Severity == sop.SeverityFail {
			code = common.ChatNoticeError
		}

		sink.sink.OnNotice(code, fmt.Sprintf("校验 %s:%s", gap.Code, gap.Detail))
	}
}
