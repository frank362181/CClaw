/*
* 记录历史记忆
* MemoryJournal,MemoryLedger的区别:
*	- MemoryLedger:本轮对话 **台账** (纯内存):并发执行器在每次工具调用前后
*	  Start/Finish,两阶段留痕,支持"已发送未返回"的审计
*
*	- MemoryJournal:会话历史的写入器,有IO,一轮开头 Begin建档
*	  结尾Finish/FinishPartial 时把Ledger.Snapshot() 一次性落库
*
* 方向是单向的:Journal 读 Ledger,反之不成立
**/

package chat

import (
	"context"
	"strings"
	"sync"

	"my.favorite/cclawservice/harness/ctxmgr"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
)

type MemoryLedger struct {
	turnRecorder *ctxmgr.TurnRecorder
}

// NewMemoryLedger MemoryLedger表示一轮对话,因此必须每轮对话都要创建一个新的
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
	sync.Once
	memory *ctxmgr.MemoryService
}

var memoryJournal = pCommon.GetSingleton(func() *MemoryJournal {
	return &MemoryJournal{}
}).Instance()

func GetMemoryJournal(memory *ctxmgr.MemoryService) IMemoryJournal {
	if memory == nil {
		panic("memory service is nil")
	}

	memoryJournal.Once.Do(func() {
		memoryJournal.memory = memory
	})
	return memoryJournal
}

func (j *MemoryJournal) Begin(_ context.Context, turnState *TurnState) (Session, error) {
	sess, recorded, err := j.memory.Begin(turnState.Cmd.Sender, turnState.Agent, ToChatRequest(turnState.Cmd))

	session := Session{
		ID:         pCommon.FirstNotBlank(sess.SessionId, turnState.Cmd.SessionId),
		AgentID:    pCommon.FirstNotBlank(sess.AgentId, turnState.Cmd.AgentID),
		Sender:     pCommon.FirstNotBlank(sess.Sender, turnState.Cmd.Sender),
		HistoryRef: sess.ChatData,

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
		turnState.Ledger.Snapshot(), //两阶段台账:含"已发起未返回"
	)
}

func (j *MemoryJournal) FinishPartial(_ context.Context, turnState *TurnState, partial string, reason dataobject.InterruptReason) int {
	return j.memory.AppendInterruptedTurn(ToTSession(turnState.Session),
		ToChatRequest(turnState.Cmd),
		partial,
		turnState.Ledger.Snapshot(),
		reason,
	)
}
