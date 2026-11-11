package chat

import (
	"context"

	"my.favorite/cclawservice/common/llmproto"
	myException "my.favorite/cclawservice/exception"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/ctxmgr"
	"my.favorite/cclawservice/harness/security"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"
)

type MemoryMaintainer struct {
	memory    *ctxmgr.MemoryService
	transport ITransportClient
}

var memoryMaintainer = pCommon.GetSingleton(func() *MemoryMaintainer {
	return &MemoryMaintainer{}
}).Instance()

func GetMemoryMaintainer(memory *ctxmgr.MemoryService, transport ITransportClient) IMemoryMaintainer {
	if memory == nil || transport == nil {
		panic("memory service or transport is nil")
	}

	memoryMaintainer.memory = memory
	memoryMaintainer.transport = transport
	return memoryMaintainer
}

func (m *MemoryMaintainer) Maintain(s Session, turnState *TurnState, asm Assembled, turnSeq int) {
	m.memory.TriggerMemoryMaintenance(ctxmgr.MaintenanceRequest{
		Session:       ToTSessionPtr(s),
		Agent:         &turnState.Agent,
		Assembled:     &asm.AssembleResult,
		Records:       ToMemoryRecords(asm.Records),
		TurnSeq:       turnSeq,
		ContextWindow: int(turnState.Provider.ContextWindow()),
	}, summarizer(turnState.Provider, m.transport))
}

func (m *MemoryMaintainer) Close(ctx context.Context, s Session, turnState *TurnState, reason int) error {
	if err := m.memory.EndSession(s.ID, reason); err != nil {
		return err
	}

	records, err := m.memory.ReadRecords(ToTSession(s))
	if err != nil {
		logging.LogWarnF("memory maintainer: read records before closing %s:%s", s.ID, err.Error())
	}

	m.memory.TriggerMemoryMaintenance(ctxmgr.MaintenanceRequest{
		Session:       ToTSessionPtr(s),
		Agent:         &turnState.Agent,
		Records:       records,
		Closing:       true,
		ContextWindow: int(turnState.Provider.ContextWindow()),
	}, summarizer(turnState.Provider, m.transport))

	return nil
}

// ============================================= 对历史对话进行摘要================================================
func summarizer(provider Provider, transport ITransportClient) ctxmgr.Summarizer {
	return func(ctx context.Context, sysPrompt, userPrompt string) (string, error) {
		// 输出额度按**窗口**收口
		maxTokens := security.CalcTurnOutputTokens(int(provider.ContextWindow()), hCommon.SummaryMaxTokens, sysPrompt, userPrompt)
		if maxTokens <= 0 {
			return pCommon.BlankString, myException.RuntimeClientKind.Newf("maintenance calls does not fit the context window,window=%d", provider.ContextWindow())
		}

		llmReply, err := transport.SimpleComplete(ctx, provider.Llm, sysPrompt, userPrompt, provider.Llm.MaxTokens)
		if err != nil {
			return llmReply.FinishReason, err
		}

		if llmReply.FinishReason == llmproto.FinishReasonLength {
			return pCommon.BlankString, myException.RuntimeClientKind.Newf("summarizer output was truncated by max_tokens (finish_reason=length)")
		}

		return llmReply.Content, nil
	}
}
