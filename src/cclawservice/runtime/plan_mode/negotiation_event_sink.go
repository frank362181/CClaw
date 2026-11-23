package plan_mode

import (
	"fmt"
	"sync/atomic"

	"my.favorite/cclawservice/common"
)

type NegotiationEventSink struct {
	sink common.ISink

	total int
	done  atomic.Int64
}

var _ IPlanEventSink = (*NegotiationEventSink)(nil)

func NewNegotiationEventSink(sink common.ISink, total int) *NegotiationEventSink {
	if sink == nil {
		sink = common.NopSink{}
	}

	return &NegotiationEventSink{
		sink:  sink,
		total: total,
	}
}

func (sink *NegotiationEventSink) OnTask(task PlanTask, state PlanTaskState) {
	label := GetTaskLabel(task)

	switch state.Status {
	case StatusRunning:
		sink.sink.OnStatus("plan.running:", label+"执行中...")
	case StatusSucceeded:
		sink.sink.OnStatus("plan done", fmt.Sprintf("%s 完成 (%d / %d)", label, sink.done.Add(1), sink.total))
	case StatusBlocked, StatusSkipped, StatusCanceled:
		sink.sink.OnStatus("plan skipped", label+"跳过")
	case StatusFailed, StatusTimeout:
		sink.sink.OnNotice(common.ChatNoticeError, fmt.Sprintf("%s 失败:%s", label, state.Err))
	}
}

func (sink *NegotiationEventSink) OnGate(task PlanTask, gaps []Gap) {
	for _, gap := range gaps {
		code := common.ChatNoticeIncomplete
		if gap.Severity == SeverityFail {
			code = common.ChatNoticeError
		}

		sink.sink.OnNotice(code, fmt.Sprintf("校验 %s:%s", gap.Code, gap.Detail))
	}
}
