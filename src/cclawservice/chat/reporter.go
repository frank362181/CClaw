package chat

import (
	"context"
	"time"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/harness/observatory"
	pCommon "my.favorite/platformutility/common"
)

type TurnReporter struct{}

var reporter = pCommon.GetSingleton(func() *TurnReporter {
	return &TurnReporter{}
}).Instance()

func GetTurnReporter() ITurnReporter {
	return reporter
}

func (TurnReporter) Begin(ctx context.Context, cmd Command) (context.Context, *TurnTiming) {
	start := pCommon.GetDatetimeUtilityInstance().GetNow()
	scope := cCommon.NormalizeCompactId(cmd.TurnID)
	return observatory.WithTurn(ctx, scope, start), &TurnTiming{TurnScope: scope}
}

func (TurnReporter) End(ctx context.Context, turnTiming *TurnTiming, firstVisibleMs int64, reply Reply, err error, startAt time.Time) {
	observatory.OnTurnTiming(ctx, ToTurnTiming(turnTiming), startAt, firstVisibleMs, reply.Content, err)
}
