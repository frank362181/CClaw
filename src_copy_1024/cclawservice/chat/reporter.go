package chat

import (
	"context"
	"time"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/harness/observatory"
	pCommon "my.favorite/platformutility/common"
)

type TurnReporter struct{}

func (TurnReporter) Begin(ctx context.Context, cmd Command) (context.Context, *TurnTiming) {
	start := pCommon.GetDatetimeUtilityInstance().GetNow()
	return observatory.WithTurn(ctx, cCommon.NormalizeCompactId(cmd.TurnID), start), &TurnTiming{}
}

func (TurnReporter) End(ctx context.Context, turnTiming *TurnTiming, firstVisibleMs int64, reply Reply, err error, startAt time.Time) {
	observatory.OnTurnTiming(ctx, ToTurnTiming(turnTiming, ctx), startAt, firstVisibleMs, reply.Content, err)
}
