package chat

import (
	"context"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/harness/ctxmgr"
)

type TurnRegistrator struct{}

func (TurnRegistrator) Register(ctx context.Context, cmd Command) (context.Context, func()) {
	runCtx, cancel := context.WithCancel(ctx)
	key := ctxmgr.BuildTurnStreamKey(cmd.Sender, cCommon.NormalizeCompactId(cmd.TurnID))

	ctxmgr.GetTurnStreamMgr().Register(key, cancel)
	return runCtx, func() { ctxmgr.GetTurnStreamMgr().Unregister(key); cancel() }
}

func (TurnRegistrator) Cancel(sender, turnID string) bool {
	key := ctxmgr.BuildTurnStreamKey(sender, cCommon.NormalizeCompactId(turnID))
	return ctxmgr.GetTurnStreamMgr().Cancel(key)
}
