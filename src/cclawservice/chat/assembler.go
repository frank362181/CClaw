/*
* 将记忆文件、SKILLs.md文件中的内容组装进上下文
*
**/

package chat

import (
	"context"

	"my.favorite/cclawservice/common/llmproto"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/ctxmgr"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"
)

type Assembler struct {
	memory *ctxmgr.MemoryService
}

var assembler = pCommon.GetSingleton(func() *Assembler {
	return &Assembler{}
}).Instance()

func GetAssembler(memory *ctxmgr.MemoryService) IContextAssembler {
	assembler.memory = memory
	return assembler
}

func (a *Assembler) Assemble(_ context.Context, turnState *TurnState, userMsg string) (Assembled, error) {
	var historicalRecords []ctxmgr.MemoryRecord

	// 未记账/无历史文件的直接跳过读磁盘
	if turnState.Session.HasHistory() {
		rs, err := a.memory.ReadRecords(ToTSession(turnState.Session))
		if err != nil {
			logging.LogWarnF("failed to read record for turn session %s: %v", turnState.Session, err)
			rs = nil
		}

		historicalRecords = rs
	}

	turnCtx := ctxmgr.TurnContext{
		Memory: turnState.SystemPrompt.DynamicMemory,
		Skills: turnState.SystemPrompt.DynamicSkills,
		Now:    pCommon.FormatCurrentTime(),
	}

	result := ctxmgr.AssembleContext(turnState.SystemPrompt.Prompt, historicalRecords, userMsg, turnCtx, turnState.Policy)

	return Assembled{
		Messages:       result.Messages,
		AssembleResult: result,
		Records:        ToChatRecords(historicalRecords),
	}, nil
}

func (a *Assembler) Fit(messages []llmproto.ChatMessage, policy hCommon.ContextPolicy) []llmproto.ChatMessage {
	return ctxmgr.FitMessages(messages, policy)
}
