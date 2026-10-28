/*
* 与Transport不同的是:
*	- 所有功能与Transport一致,只是加上了日志
*
*	使用了decorate设计模式,装饰器只是拿到了(开始时间、结束时间、最终结果),看不到
*	首个data行、也拿不到流式输出的中间的usage末包 -- 这两项必须留在streamOnce内
**/

package chat

import (
	"context"
	"time"

	"my.favorite/cclawservice/common/llmproto"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"
)

type TransportLogger struct {
	transporter ITransportClient
}

func NewTransportLogger(transporter ITransportClient) *TransportLogger {
	return &TransportLogger{transporter: transporter}
}

func (tl *TransportLogger) Complete(ctx context.Context, p Provider, req llmproto.LlmRequest) (llmproto.LlmReply, error) {
	start := pCommon.GetDatetimeUtilityInstance().GetNow()
	reply, err := tl.transporter.Complete(ctx, p, req)
	tl.log(p, false, start, reply, err)
	return reply, err
}

func (tl *TransportLogger) Stream(ctx context.Context, p Provider, req llmproto.LlmRequest, ob IStreamObserver) (llmproto.LlmReply, error) {
	start := pCommon.GetDatetimeUtilityInstance().GetNow()
	reply, err := tl.transporter.Stream(ctx, p, req, ob)
	tl.log(p, true, start, reply, err)
	return reply, err
}

func (tl *TransportLogger) log(p Provider, stream bool, start time.Time, reply llmproto.LlmReply, err error) {
	elapsed := pCommon.MilliDiff(start)
	if err != nil {
		logging.LogWarnF("llm gateway: provider=%s stream=%v elapsed=%dms tools=%d err=%v",
			p.Identifier(), stream, elapsed, len(reply.ToolCalls), err)
		return
	}

	logging.LogDebugF("llm gateway: provider=%s stream=%v elapsed=%dms finish=%s tools=%d",
		p.Identifier(), stream, elapsed, reply.FinishReason, len(reply.ToolCalls))
}
