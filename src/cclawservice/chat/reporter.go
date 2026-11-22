package chat

import (
	"context"
	"time"

	chatCommon "my.favorite/cclawservice/chat/common"
	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/harness/observatory"
	pCommon "my.favorite/platformutility/common"
)

type TurnReporter struct{}

var reporter = pCommon.GetSingleton(func() *TurnReporter {
	return &TurnReporter{}
}).Instance()

func GetTurnReporter() chatCommon.ITurnReporter {
	return reporter
}

func (TurnReporter) Begin(ctx context.Context, cmd chatCommon.Command) (context.Context, *chatCommon.TurnTiming) {
	start := pCommon.GetDatetimeUtilityInstance().GetNow()
	scope := cCommon.NormalizeCompactId(cmd.TurnID)
	return observatory.WithTurn(ctx, scope, start), &chatCommon.TurnTiming{TurnScope: scope}
}

func (TurnReporter) End(ctx context.Context, turnTiming *chatCommon.TurnTiming, firstVisibleMs int64, reply chatCommon.Reply, err error, startAt time.Time) {
	observatory.OnTurnTiming(ctx, ToTurnTiming(turnTiming), startAt, firstVisibleMs, reply.Content, err)
}
