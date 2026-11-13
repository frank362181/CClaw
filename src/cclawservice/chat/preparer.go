/**
* 把Command变成本轮可执行的TurnState
* 	- 要读取DB
*	- 读取技能文件
*	- 合成工具(tool)、调用LLM分类
*	- 只保留TurnState 长什么样 + 怎么构造
**/

package chat

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"my.favorite/cclawservice/common/llmproto"
	myException "my.favorite/cclawservice/exception"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/ctxmgr"
	"my.favorite/cclawservice/harness/security"
	"my.favorite/cclawservice/harness/toolexecutor"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/model/repo"
	pCommon "my.favorite/platformutility/common"
	pException "my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
)

type Preparer struct {
	agentRepo     *repo.AgentRepository
	agentRouter   *ctxmgr.AgentRouter
	agentDocCache *ctxmgr.AgentDocumentCache
	toolsSearch   *ToolsSearch

	functionCalling bool

	warned sync.Map // agentId,配置异味,仅报一次

	classifier hCommon.ClassifyAgent
}

var chatPreparer = pCommon.GetSingleton(func() *Preparer {
	return &Preparer{
		agentRepo:     nil,
		agentRouter:   nil,
		agentDocCache: ctxmgr.GetAgentDocCache(),
		toolsSearch:   GetToolsSearch(),
	}
}).Instance()

func GetChatPreparer(agentRepo *repo.AgentRepository, functionCalling bool, classifier hCommon.ClassifyAgent) ITurnPreparer {
	if agentRepo == nil {
		panic("GetChatPreparer called with nil agentRepo or router")
	}

	chatPreparer.agentRepo = agentRepo
	chatPreparer.agentRouter = ctxmgr.GetAgentRouter(agentRepo)

	chatPreparer.functionCalling = functionCalling
	chatPreparer.classifier = classifier // 基于LLM的分类器

	return chatPreparer
}

func (p *Preparer) Prepare(ctx context.Context, cmd Command) (*TurnState, error) {
	t0 := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().MilliTimestamp
	agent, err := p.agentRouter.Resolve(ctx, cmd.ComCode, cmd.SessionId, cmd.Message, cmd.AgentID, p.classifier)
	if err != nil {
		return nil, err
	}

	turn, err := p.PrepareFor(ctx, cmd, agent, TurnRoleRoot)
	if err != nil {
		return nil, err
	}

	turn.Timing.RouteMs = pCommon.MilliDiff2(t0)
	turn.Timing.AgentId = cmd.AgentID //权威来源是路由结果

	return turn, err
}

// PrepareFor 根据制定的agent构造TurnState(不走路由)
// worker/evaluator的目标agent由引擎决定,不能再路由一次
func (p *Preparer) PrepareFor(ctx context.Context, cmd Command, agent dataobject.TAgent, turnRole TurnRole) (*TurnState, error) {
	_ = ctx
	_ = turnRole

	timing := &TurnTiming{}

	// provider
	llmProvider, err := p.resolveProvider(cmd.ComCode, agent)
	if err != nil {
		return nil, err
	}

	// 技能 + 工具
	t1 := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().MilliTimestamp
	skillDocs, report := p.agentDocCache.LoadSkills(p.collectSkillDirs(agent))
	report.LogSummary(fmt.Sprintf("chatting service: agent=%s,turn=%s", agent.AgentId, cmd.TurnID))
	bundle := p.toolsSearch.GetToolsBundle(skillDocs)
	timing.SkillMs = pCommon.MilliDiff2(t1)

	// 意图门 + prompt + plan + budget
	t2 := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().MilliTimestamp
	gate := bundle.Activate(cmd.Message)
	sysPrompt, err := ctxmgr.BuildSystemPromptWithSkills(agent, skillDocs, hCommon.BootstrapScopeNormal, gate.ActiveFiles, bundle.PromptItems())
	if err != nil {
		return nil, myException.ChatLLMKind.Newf("building system prompt failed: %s", err.Error())
	}

	visible := bundle.VisibleTools(gate)
	var tTool IRoundTool
	if p.functionCalling {
		tTool = NewRoundToolAdapter(BuildFirstRoundTool(visible, gate.ForcedTools, GetLlmProviderThinking(llmProvider)),
			fmt.Sprintf("agent=%s,turn=%s", agent.AgentId, cmd.TurnID))
	} else {
		visible, tTool = nil, nil
	}

	reserve := security.EstimateToolReserveTokens(visible)
	timing.PromptMs = pCommon.MilliDiff2(t2)

	policy := ctxmgr.GetContextPolicy(agent, int(llmProvider.ContextWindow))
	policy.ToolsReserveTokens = reserve
	p.warnPolicyIssues(agent, llmProvider, sysPrompt, reserve)

	workRoot, _ := hCommon.BuildAgentBootstrapRoot(agent)

	return &TurnState{
		Cmd:           cmd,
		Agent:         agent,
		Provider:      Provider{Llm: llmProvider},
		SystemPrompt:  *sysPrompt,
		VisibleTools:  visible,
		TurnTool:      tTool,
		Policy:        policy,
		ToolsMaxRunes: ctxmgr.TurnToolResultMaxRunes,
		Scripts:       toolexecutor.GetScriptsResolver(bundle.Specs),
		WorkRoot:      workRoot,
		Timing:        timing,
		Ledger:        NewMemoryLedger(ctxmgr.NewTurnRecorder()), //创建本轮对话所需的账本,两阶段对话
		Scope:         fmt.Sprintf("agent=%s,turn=%s", agent.AgentId, cmd.TurnID),
	}, nil
}

// ========================== private functions & methods =======================

func (p *Preparer) resolveProvider(comCode string, agent dataobject.TAgent) (dataobject.TLlm, error) {
	modelIDs := make([]string, 0, 4)
	if id := strings.TrimSpace(agent.PrimaryModel); id != pCommon.BlankString {
		modelIDs = append(modelIDs, id)
	}

	// 没有设置primary时,取fallback
	for _, fb := range pCommon.SplitStringEx(agent.Fallbacks, ",") {
		if id := strings.TrimSpace(fb); id != pCommon.BlankString {
			modelIDs = append(modelIDs, id)
		}
	}

	providers := p.agentRepo.GetLLMProviders(comCode)
	for _, modelID := range modelIDs {
		for _, pr := range providers {
			if pr.ProviderId == modelID && !pr.Disabled && !pr.Deleted {
				return pr, nil
			}
		}
	}

	return dataobject.TLlm{}, pException.UnMatchErrorEx2("no available provider for agent %s", agent.AgentId)
}

func (p *Preparer) collectSkillDirs(agent dataobject.TAgent) []string {
	skills := p.agentRepo.GetSkillsByAgent(agent)
	if len(skills) == 0 {
		return nil
	}

	dirs := make([]string, 0, len(skills))
	for i := range skills {
		if dir := strings.TrimSpace(skills[i].SkillDir); dir != pCommon.BlankString {
			dirs = append(dirs, dir)
		}
	}

	return dirs
}

// 报告"上下文预算"明显不合理的配置(每个agent仅仅报一次)
func (p *Preparer) warnPolicyIssues(agent dataobject.TAgent, llmProvider dataobject.TLlm, sysPrompt *hCommon.SystemPrompt, toolsReserve int) {
	window := int(llmProvider.ContextWindow)
	if window <= 0 {
		return
	}

	if _, warned := p.warned.Load(agent.AgentId); warned {
		return
	}

	policy := ctxmgr.GetContextPolicy(agent, window)
	policy.ToolsReserveTokens = toolsReserve

	systemTokens := security.EstimateInputTokens([]llmproto.ChatMessage{
		{Role: hCommon.RoleSystem, Content: sysPrompt.Prompt},
	})

	for _, issue := range security.PolicyWarnings(policy, systemTokens) {
		logging.LogWarnF("chat: agent = %s context budget issues: %s", agent.AgentId, issue)
	}

	p.warned.Store(agent.AgentId, struct{}{})
}

var _ ITurnPreparer = (*Preparer)(nil)
