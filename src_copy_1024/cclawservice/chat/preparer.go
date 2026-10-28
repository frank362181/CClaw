package chat

import (
	"context"
	"strings"
	"sync"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/harness/ctxmgr"
	"my.favorite/cclawservice/model/repo"
	"my.favorite/cclawservice/request"
	pCommon "my.favorite/platformutility/common"
	pException "my.favorite/platformutility/exception"
)

type Preparer struct {
	repo   *repo.AgentRepository
	router *ctxmgr.AgentRouter
	docs   *ctxmgr.AgentDocumentCache
	//tools  *tool.ToolsSearch

	warned sync.Map
}

func (p *Preparer) NewCommand(comCode, sender string, chatReq *request.ChatRequest) (*Command, error) {
	msg := strings.TrimSpace(chatReq.ReqMsg)
	if len(msg) == 0 {
		return nil, pException.ParameterEmptyErrorEx("chatting message is empty")
	}

	if len(msg) > 4096 {
		return nil, pException.ParameterEmptyErrorEx("chatting message is too long")
	}

	// sessionId:统一带单位前缀,且对回传服务端签发 id幂等
	prefix := cCommon.NormalizeCompactId(comCode)
	sessionId := cCommon.NormalizeCompactId(chatReq.SessionId)
	if sessionId == "" {
		sessionId = cCommon.NewCompactId(prefix)
	} else {
		sessionId = cCommon.NormalizeCompactId(prefix + strings.TrimPrefix(sessionId, prefix))
	}

	turnId := cCommon.NormalizeCompactId(chatReq.TurnId)
	if turnId == "" {
		turnId = cCommon.NewCompactId(pCommon.BlankString)
	}

	genAt := chatReq.GenAt
	if genAt <= 0 {
		genAt = pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	}

	chatType, channel := chatReq.ChatType, chatReq.ChatChannel
	if chatType == 0 {
		chatType = cCommon.ChatTypePrivate
	}
	if channel == 0 {
		channel = cCommon.ChatChannelWeb
	}

	return &Command{
		Sender: sender, ComCode: comCode,
		SessionId: sessionId, TurnID: turnId,
		Message: msg,
		AgentID: chatReq.AgentId, ChatType: chatType, ChatChannel: channel, GenAt: genAt,
	}, nil
}

func (p *Preparer) Prepare(ctx context.Context, cmd Command) (*TurnState, error) {
	//timing := &TurnTiming{}
	//
	//// 路由
	//t0 := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	//agent, err := p.router.Resolve(ctx, cmd.ComCode, cmd.SessionId, cmd.Message, cmd.AgentID, api.NewClient().Complete)
	//timing.RouteMs = pCommon.MilliDiff2(t0)
	//if err != nil {
	//	return nil, err
	//}
	//
	//// provider
	//llmProvider, err := p.ResolveProvider(cmd.ComCode, agent)
	//if err != nil {
	//	return nil, err
	//}
	//
	//// 技能 + 工具
	//t1 := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	//skillDocs, report := p.docs.LoadSkills(p.collectSkillDirs(agent))
	//report.LogSummary(fmt.Sprintf("chatting service: agent=%s,turn=%s", agent.AgentId, cmd.TurnID))
	//bundle := p.tools.GetToolsBundle(skillDocs)
	//timing.SkillMs = pCommon.MilliDiff2(t1)
	//
	//// 意图门 + prompt + plan + budget
	//t2 := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	//gate := bundle.Activate(cmd.Message)
	//sysPrompt, err := ctxmgr.BuildSystemPromptWithSkills(agent, skillDocs, hCommon.BootstrapScopeNormal, gate.ActiveFiles, bundle.PromptItems())
	//if err != nil {
	//	return nil, myException.ChatLLMKind.Newf("building system prompt failed: %s", err.Error())
	//}
	//
	//visible := bundle.VisibleTools(gate)
	//plan := tool.NewRoundPllanerAdapter(tool.BuildFirstRoundToolPlan(visible, gate.ForcedTools),
	//	fmt.Sprintf("agent=%s,turn=%s", agent.AgentId, cmd.TurnID))
	//
	//reserve := security.EstimateToolReserveTokens(visible)
	//timing.PromptMs = pCommon.MilliDiff2(t2)
	//
	//policy := ctxmgr.GetContextPolicy(agent, int(llmProvider.ContentWindow))
	//policy.ToolsReserveTokens = reserve
	//p.warnPolicyIssues(agent, llmProvider, sysPrompt, reserve)
	//
	//workRoot, _ := hCommon.BuildAgentBootstrapRoot(agent)
	//
	//return &TurnState{
	//	Cmd: cmd, Agent: agent,
	//	AgentRef:      *NewAgentReference(agent),
	//	Provider:      Provider{Llm: llmProvider},
	//	SystemPrompt:  *sysPrompt,
	//	VisibleTools:  visible,
	//	Plan:          plan,
	//	Policy:        policy,
	//	ToolsMaxRunes: ctxmgr.TurnToolResultMaxRunes,
	//	Scripts:       ScriptsOf(bundle.Scripts),
	//	WorkRoot:      workRoot,
	//	Timing:        timing,
	//	Ledger:        NewMemoryLedger(),
	//	Scope:         fmt.Sprintf("agent=%s,turn=%s", agent.AgentId, cmd.TurnID),
	//}, nil

	return nil, nil
}
