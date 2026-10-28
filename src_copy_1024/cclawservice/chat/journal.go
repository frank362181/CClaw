/*
* 记录历史记忆
* MemoryJournal,MemoryLedger的区别:
*	- MemoryJournal:表示一个记忆账本;MemoryLedger表示一轮聊天的记忆
**/

package chat

import (
	"context"
	"strings"

	"my.favorite/cclawservice/harness/ctxmgr"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
)

type MemoryLedger struct {
	turnRecorder *ctxmgr.TurnRecorder
}

func NewMemoryLedger(turnRecorder *ctxmgr.TurnRecorder) *MemoryLedger {
	return &MemoryLedger{turnRecorder: turnRecorder}
}

func (l *MemoryLedger) Start(name string, at int64) int {
	return l.turnRecorder.Start(name, at)
}

func (l *MemoryLedger) Finish(index int, at int64, content string) {
	l.turnRecorder.Finish(index, at, content)
}

func (l *MemoryLedger) Snapshot() []ctxmgr.TurnToolRecord {
	return l.turnRecorder.Snapshot()
}

type MemoryJournal struct {
	memory *ctxmgr.MemoryService
}

func (j *MemoryJournal) Begin(_ context.Context, turnState *TurnState) (Session, error) {
	sess, recorded, err := j.memory.Begin(turnState.Cmd.Sender, turnState.Agent, ToChatRequest(turnState.Cmd))

	session := Session{
		ID:         pCommon.FirstNotBlank(sess.SessionId, turnState.Cmd.SessionId),
		AgentID:    pCommon.FirstNotBlank(sess.AgentId, turnState.Cmd.AgentID),
		Sender:     pCommon.FirstNotBlank(sess.Sender, turnState.Cmd.Sender),
		HistoryRef: HistoryAgentReference(sess.ChatData),

		// 只有DB里真有这一行才允许记账
		Recorded: recorded && strings.TrimSpace(sess.SessionId) != pCommon.BlankString,
	}

	if err != nil {
		session.Recorded = false // 归属冲突:本轮不记账,但仍回话
	}

	if !session.Valid() {
		// 防御:走到这里说明 Begin的契约被破坏了(recorded=true,但无id)
		// 降级为不记账,但不能让后续 AppendTurn静默返回 0
		session.Recorded = false
	}

	return session, err
}

func (j *MemoryJournal) Finish(_ context.Context, turnState *TurnState, context string) int {
	return j.memory.AppendTurn(
		ToTSession(turnState.Session),
		ToChatRequest(turnState.Cmd),
		context,
		turnState.Ledger.(*MemoryLedger).Snapshot(), //两阶段台账:含"已发起未返回"
	)
}

func (j *MemoryJournal) FinishPartial(_ context.Context, turnState *TurnState, partial string, reason dataobject.InterruptReason) int {
	return j.memory.AppendInterruptedTurn(ToTSession(turnState.Session),
		ToChatRequest(turnState.Cmd),
		partial,
		turnState.Ledger.(*MemoryLedger).Snapshot(),
		reason,
	)
}
