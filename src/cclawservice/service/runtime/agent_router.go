/**
* Agent 内容路由器（方案：用户显式选择 + 根据内容推理 + 兜底）
*
* 职责:在用户未显式指定 Agent 时,根据消息内容选择最合适的 Agent,
* 使"不良资产智能助手"等专用 Agent 能在用户未手动选择时也被自动命中。
*
* 解析优先级(逐级回落,最终必有兜底):
*   1. 显式 agentId:用户/前端明确指定;未命中或禁用直接返回错误(与旧行为一致);
*   2. 会话绑定:同一会话首次路由后,后续消息沿用(保证上下文一致,避免重复路由);
*   3. 关键词规则:命中数据库(t_agent_route_rule)配置的关键词 → 路由到对应 Agent(快、零 LLM 成本);
*   4. LLM 分类:轻量调用让模型从"Agent 名称+描述"中挑选最合适者(输出严格校验);
*   5. 默认 Agent:以上均未命中时的兜底(IsDefault,否则取第一个可用)。
*
* 安全/健壮性/性能:
*   - LLM 分类结果只接受"已配置 AgentId 的精确匹配",其余一律回落默认,绝不信任模型自由文本;
*   - LLM 分类带超时,失败/超时/无可用 provider 时静默回落默认,不阻断聊天;
*   - 关键词为子串匹配 O(n),命中即短路;会话绑定使路由只在每个会话首次消息执行一次;
*   - 仅一个可用 Agent 时直接短路返回,不做无谓的路由计算。
**/

package runtime

import (
	"context"
	"sort"
	"strings"
	"time"

	myCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/model/repo"
	"my.favorite/platformutility/common"
	pException "my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
)

// AgentRouter 负责将一条用户消息解析到目标 Agent。
type AgentRouter struct {
	agentRepo     repo.IAgentRepository
	llmClient     *Client
	sessionAgents *ChatSessionAgentRegistry

	llmRoutingOn bool          // 是否启用 LLM 智能分类路由
	llmTimeout   time.Duration // LLM 分类超时
	llmMaxTokens int           // LLM 分类输出 token 上限
}

var agentRouter = common.GetSingleton(func() *AgentRouter {
	return &AgentRouter{
		agentRepo:     nil,
		llmClient:     NewClient(),
		sessionAgents: GetChatSessionAgentRegistry(),
		llmRoutingOn:  myCommon.AgentRoutingLLMEnabled,
		llmTimeout:    time.Duration(myCommon.AgentRoutingLLMTimeoutSeconds) * time.Second,
		llmMaxTokens:  myCommon.AgentRoutingLLMMaxTokens,
	}
}).Instance()

// GetAgentRouter 单例模式。
func GetAgentRouter(agentRepo repo.IAgentRepository) *AgentRouter {
	agentRouter.agentRepo = agentRepo
	return agentRouter
}

// Resolve 解析本次会话的目标 Agent(混合路由)。
//   - 返回 error 仅发生在:显式指定但 Agent 不存在/禁用,或该 comCode 下无任何可用 Agent;
//   - 命中(含默认兜底)后会把结果绑定到会话,保证会话内上下文一致。
func (r *AgentRouter) Resolve(ctx context.Context, comCode, sessionId, reqMsg, explicitAgentId string) (dataobject.TAgent, error) {
	agents := r.availableAgents(comCode) //获取当前comCode的agent
	if len(agents) == 0 {
		return dataobject.TAgent{}, pException.UnMatchErrorEx2("no available chat agent for comCode:%s", comCode)
	}

	// 0. 只有一个可用 Agent:直接命中,无需路由计算
	if len(agents) == 1 {
		r.bind(comCode, sessionId, agents[0].AgentId)
		return agents[0], nil
	}

	// 1. 显式指定:未命中/禁用直接报错(与旧行为一致),命中后绑定会话
	if id := strings.TrimSpace(explicitAgentId); id != "" {
		if agent, ok := r.findAgent(agents, id); ok {
			r.bind(comCode, sessionId, agent.AgentId)
			return agent, nil
		}
		return dataobject.TAgent{}, pException.UnMatchErrorEx2("chat agent not found: %s", id)
	}

	// 2. 会话绑定:同一会话后续消息沿用首次路由结果
	if id, ok := r.sessionAgents.Get(comCode, sessionId); ok {
		if agent, ok := r.findAgent(agents, id); ok {
			return agent, nil
		}
		r.sessionAgents.ClearKey(comCode, sessionId) // 绑定目标已失效,重新路由
	}

	// 3. 关键词规则(数据库配置)
	if agent, ok := r.matchKeyword(agents, comCode, reqMsg); ok {
		r.bind(comCode, sessionId, agent.AgentId)
		return agent, nil
	}

	// 4. LLM 分类(可选;失败/未命中静默回落)
	if r.llmRoutingOn {
		if agent, ok := r.classifyByLLM(ctx, comCode, agents, reqMsg); ok {
			r.bind(comCode, sessionId, agent.AgentId)
			return agent, nil
		}
	}

	// 5. 默认兜底
	agent := r.defaultAgent(agents)
	r.bind(comCode, sessionId, agent.AgentId)
	return agent, nil
}

// ============================== private methods ==============================

// availableAgents 返回该 comCode 下可用(未删除、未禁用)的 Agent 列表。
func (r *AgentRouter) availableAgents(comCode string) []dataobject.TAgent {
	all := r.agentRepo.GetAgents(comCode)
	out := make([]dataobject.TAgent, 0, len(all))
	for _, a := range all {
		if !a.Deleted && !a.Disabled {
			out = append(out, a)
		}
	}
	return out
}

// findAgent 在列表中按 agentId 精确查找。
func (r *AgentRouter) findAgent(agents []dataobject.TAgent, agentId string) (dataobject.TAgent, bool) {
	for _, a := range agents {
		if a.AgentId == agentId {
			return a, true
		}
	}
	return dataobject.TAgent{}, false
}

// defaultAgent 取默认(IsDefault)Agent,否则取第一个可用 Agent。
func (r *AgentRouter) defaultAgent(agents []dataobject.TAgent) dataobject.TAgent {
	for _, a := range agents {
		if a.IsDefault {
			return a
		}
	}
	return agents[0]
}

// bind 将会话绑定到指定 Agent(空 sessionId 时忽略,不做绑定)。
func (r *AgentRouter) bind(comCode, sessionId, agentId string) {
	r.sessionAgents.Set(comCode, sessionId, agentId)
}

// matchKeyword 按数据库关键词规则匹配:优先级高的规则优先;命中任一关键词即路由到目标 Agent。
// 规则的目标 Agent 不可用时跳过该规则(避免路由到已删除/禁用的 Agent)。
func (r *AgentRouter) matchKeyword(agents []dataobject.TAgent, comCode, reqMsg string) (dataobject.TAgent, bool) {
	if strings.TrimSpace(reqMsg) == "" {
		return dataobject.TAgent{}, false
	}

	rules := r.agentRepo.GetAgentRouteRules(comCode)
	if len(rules) == 0 {
		return dataobject.TAgent{}, false
	}
	// 按优先级降序,保证高优先级规则先参与匹配
	sort.SliceStable(rules, func(i, j int) bool {
		return rules[i].Priority > rules[j].Priority
	})

	for _, rule := range rules {
		if rule.Disabled || rule.Deleted {
			continue
		}
		if _, ok := r.findAgent(agents, rule.AgentId); !ok {
			continue // 目标 Agent 不可用,跳过该规则
		}
		for _, kw := range common.SplitKeyWords(rule.Keywords) {
			if kw != "" && strings.Contains(reqMsg, kw) {
				agent, _ := r.findAgent(agents, rule.AgentId)
				return agent, true
			}
		}
	}
	return dataobject.TAgent{}, false
}

// classifyByLLM 用 LLM 从可用 Agent 中挑选最合适者。
// 输出仅接受"已配置 AgentId 的精确匹配",否则视为未命中(回落默认),绝不信任模型自由文本。
func (r *AgentRouter) classifyByLLM(ctx context.Context, comCode string, agents []dataobject.TAgent, reqMsg string) (dataobject.TAgent, bool) {
	if strings.TrimSpace(reqMsg) == "" {
		return dataobject.TAgent{}, false
	}
	provider, ok := r.resolveRoutingProvider(comCode)
	if !ok {
		return dataobject.TAgent{}, false // 无可用 provider,静默回落
	}

	// 分类调用独立超时,避免拖慢主聊天流程
	classifyCtx, cancel := context.WithTimeout(ctx, r.llmTimeout)
	defer cancel()

	// 每个候选 Agent 的"简介"优先取工作区 AGENTS.md 的 front matter summary,
	// 无摘要时回退数据库 description
	candidates := make([]dataobject.ClassifyAgent, 0, len(agents))
	for i := range agents {
		agent := &agents[i]
		candidates = append(candidates, dataobject.ClassifyAgent{
			AgentId:   agent.AgentId,
			AgentName: agent.AgentName,
			Label:     r.classifyLabel(*agent),
		})
	}

	content, err := r.llmClient.Complete(classifyCtx, provider, BuildClassifySystemPrompt(candidates), reqMsg, r.llmMaxTokens)
	if err != nil {
		logging.LogDebugF("agent router: runtime classify skipped, err:%s", err.Error())
		return dataobject.TAgent{}, false
	}

	// 严格校验:只接受精确匹配的 AgentId(忽略首尾空白,大小写不敏感)
	for _, a := range agents {
		if strings.EqualFold(strings.TrimSpace(content), a.AgentId) {
			return a, true
		}
	}
	logging.LogDebugF("agent router: runtime classify not matched, raw:%s", content)
	return dataobject.TAgent{}, false
}

// resolveRoutingProvider 取路由分类使用的 LLM Provider。
// 优先使用默认 Agent 的模型链(主模型/兜底,与 chat_service.resolveProvider 一致);
// 默认 Agent 无可用 Provider 时,回退到该 comCode 下任意一个可用 Provider 来做分类,
// 避免 LLM 意图判断被"无 provider"静默跳过(分类只输出一个短 AgentId,用哪个模型均可)。
func (r *AgentRouter) resolveRoutingProvider(comCode string) (dataobject.TLlm, bool) {
	agents := r.availableAgents(comCode)
	if len(agents) == 0 {
		return dataobject.TLlm{}, false
	}
	def := r.defaultAgent(agents)

	modelIds := make([]string, 0, 4)
	if id := strings.TrimSpace(def.PrimaryModel); id != "" {
		modelIds = append(modelIds, id)
	}
	for _, fb := range strings.Split(def.Fallbacks, ",") {
		if id := strings.TrimSpace(fb); id != "" {
			modelIds = append(modelIds, id)
		}
	}

	providers := r.agentRepo.GetLLMProviders(comCode)
	for _, modelId := range modelIds {
		if p, ok := findAvailableProvider(providers, modelId); ok {
			return p, true
		}
	}

	// 兜底:默认 Agent 无可用 Provider 时,用任意一个可用 Provider 完成分类
	return firstAvailableProvider(providers)
}

// classifyLabel 解析某 Agent 在路由意图判断中使用的"简介"。
// 优先级:工作区 AGENTS.md 的 front matter summary > 数据库 description > Agent 名称。
// AGENTS.md 缺失/无摘要/读取失败时静默降级,不影响路由可用性。
func (r *AgentRouter) classifyLabel(agent dataobject.TAgent) string {
	if label := r.agentDocSummary(agent); label != "" {
		return label
	}
	if desc := strings.TrimSpace(agent.Description); desc != "" {
		return desc
	}
	return strings.TrimSpace(agent.AgentName)
}

// agentDocSummary 读取 agent 工作区 AGENTS.md 的 front matter summary。
// 复用 PromptRepository 的指纹缓存(文件未变零读盘);读取失败或缺少摘要返回空串。
func (r *AgentRouter) agentDocSummary(agent dataobject.TAgent) string {
	promptRepo := repo.GetPromptRepository()
	if err := promptRepo.LoadAgentBootstrap(agent); err != nil {
		return ""
	}
	for _, doc := range promptRepo.GetAllDocuments(agent.AgentId) {
		if doc.Kind == dataobject.AgentDocAgent {
			return strings.TrimSpace(doc.FrontMatter.Summary)
		}
	}
	return ""
}

// findAvailableProvider 按 providerId 查找可用(未删除、未禁用)的 Provider。
func findAvailableProvider(providers []dataobject.TLlm, providerId string) (dataobject.TLlm, bool) {
	for _, p := range providers {
		if p.ProviderId == providerId && !p.Disabled && !p.Deleted {
			return p, true
		}
	}
	return dataobject.TLlm{}, false
}

// firstAvailableProvider 返回第一个可用(未删除、未禁用)的 Provider。
func firstAvailableProvider(providers []dataobject.TLlm) (dataobject.TLlm, bool) {
	for _, p := range providers {
		if !p.Disabled && !p.Deleted {
			return p, true
		}
	}
	return dataobject.TLlm{}, false
}
