/**
* 流式对话场景下:以发起者和轮次组成的关键字保存流式对话,用来流式对话中时取消
*
**/

package chat

import (
	"context"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/harness/ctxmgr"
	pCommon "my.favorite/platformutility/common"
)

type TurnRegistrator struct{}

var chatTurnRegistrator = pCommon.GetSingleton(func() *TurnRegistrator {
	return &TurnRegistrator{}
}).Instance()

func GetChatTurnRegistrator() ITurnRegistrator {
	return chatTurnRegistrator
}

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
