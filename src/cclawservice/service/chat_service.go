/**
* Chat 业务服务
*
* 职责:接收 cclawchat 聊天界面发来的消息,调用 LLM 引擎返回真实回复。
*
* 处理流程:
*   1. 校验消息内容并做长度限制,解析/生成会话标识
*   2. 发送者以服务端认证信息为准(不信任客户端传入的 senderId,防伪造)
*   3. 解析目标 Agent(请求体 agentId 优先,缺省取默认 Agent)
*   4. 解析 LLM Provider(Agent.PrimaryModel 优先,失败回退 Fallbacks)
*   5. 基于 Agent 工作区启动文档组装 system prompt
*   6. 调用 LLM chat/completions 取回真实回复
*
* 与 LoginService/AgentService 保持一致:组合 baseService.BaseService + 工厂 GetChatService()。
**/

package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/controller/model"
	myException "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/model/repo"
	"my.favorite/cclawservice/service/llm"
	myUtility "my.favorite/cclawservice/utility"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/database/core"
	pException "my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
	baseService "my.favorite/platformutility/webbase/middleware/service"
)

// MaxChatMessageLen 单条聊天消息的字符上限(防超大请求体滥用)
const MaxChatMessageLen = 4096

// ChatService 处理聊天消息:校验 → 解析 Agent/Provider → 组装 prompt → 调用 LLM。
type ChatService struct {
	baseService.BaseService

	agentRepo     repo.IAgentRepository
	promptService *PromptService
	llmClient     *llm.Client
}

// GetChatService 构建聊天服务。
//   - db:  用于读取 Agent / LLM Provider / Skill 配置(与 AgentService 共用同一仓库单例)
func GetChatService(db core.IBaseDatabase) *ChatService {
	return &ChatService{
		BaseService: baseService.BaseService{
			Name:        "chatService",
			Description: "receive chat messages and reply via the LLM engine",
		},
		agentRepo:     repo.GetAgentRepository(db),
		promptService: GetPromptService(),
		llmClient:     llm.NewClient(),
	}
}

// SendMessage 接收并应答一条聊天消息(同步模式:等待完整回复后一次性返回)。
//   - sender: 服务端认证得到的登录名(由认证 interceptor 写入请求上下文),覆盖请求体 senderId 防伪造
//   - comCode: 服务端认证得到的单位编码,用于 Agent/Provider 的归属隔离(owner_id)
func (s *ChatService) SendMessage(ctx context.Context, req *model.ChatRequest, sender, comCode string) (*model.ChatResponse, error) {
	req, systemPrompt, llmProvider, err := s.prepareChat(req, sender, comCode)
	if err != nil {
		return nil, err
	}

	// 调用 LLM 引擎取回完整回复
	reply, err := s.llmClient.ChatCompletion(ctx, llmProvider, systemPrompt, req)
	if err != nil {
		return nil, s.llmError(err, "chat service: llm call error")
	}

	// 组装应答
	now := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	resp := &model.ChatResponse{
		SenderId:    req.SenderId,
		SessionId:   req.SessionId,
		ChatType:    req.ChatType,
		ChatChannel: req.ChatChannel,
		ChatId:      now,
		Reply:       reply,
	}
	return resp, nil
}

// ChatStream 以流式(SSE)方式应答聊天消息。
//   - onChunk:  每个增量文本到达时被调用(参数为该次增量 delta,由控制器转发给浏览器)
//   - onStatus: 内容开始前报告 Agent 所处阶段(如解析任务/思考),由控制器以 status 事件转发
//   - 返回值:   完整回复文本
//   - 与 SendMessage 共用 prepareChat,保证同步/流式两条路径的校验与解析完全一致
func (s *ChatService) ChatStream(ctx context.Context, req *model.ChatRequest, sender, comCode string, onChunk func(string), onStatus func(status, text string)) (string, error) {
	if onStatus != nil {
		onStatus(cCommon.ChatStatusPreparing, "正在解析任务…")
	}
	req, systemPrompt, llmProvider, err := s.prepareChat(req, sender, comCode)
	if err != nil {
		return "", err
	}

	if onStatus != nil {
		onStatus(cCommon.ChatStatusThinking, "正在思考…")
	}
	reply, err := s.llmClient.ChatCompletionStream(ctx, llmProvider, systemPrompt, req, onChunk)
	if err != nil {
		return "", s.llmError(err, "chat service: llm stream error")
	}
	return reply, nil
}

// =============================== private functions & methods========================
// prepareChat 执行同步/流式两条聊天路径的公共前置步骤:
// 校验/规整入参 → 覆盖 sender → 生成 sessionId → 缺省类型 → 解析 Agent/Provider → 组装 system prompt。
// 返回规范化后的请求、系统提示词与解析出的 Provider,供 SendMessage/ChatStream 复用,避免逻辑重复。
func (s *ChatService) prepareChat(req *model.ChatRequest, sender, comCode string) (*model.ChatRequest, *dataobject.SystemPrompt, dataobject.TLlm, error) {
	if req == nil {
		return nil, nil, dataobject.TLlm{}, pException.ParameterEmptyErrorEx("chat request is nil")
	}

	// 1. 校验消息内容
	req.ReqMsg = strings.TrimSpace(req.ReqMsg)
	if req.ReqMsg == "" {
		return nil, nil, dataobject.TLlm{}, pException.ParameterEmptyErrorEx("chat message is empty")
	}
	if len([]rune(req.ReqMsg)) > MaxChatMessageLen {
		return nil, nil, dataobject.TLlm{}, pException.ParameterEmptyErrorEx("chat message is too long")
	}

	// 2. 发送者以服务端认证信息为准
	if strings.TrimSpace(sender) != "" {
		req.SenderId = strings.TrimSpace(sender)
	}

	// 3. 会话标识:客户端可传入;为空则由服务端生成
	if strings.TrimSpace(req.SessionId) == "" {
		req.SessionId = pCommon.GenerateUUID()
	}

	// 4. 缺省类型
	if req.ChatType == 0 {
		req.ChatType = cCommon.ChatTypePrivate
	}
	if req.ChatChannel == 0 {
		req.ChatChannel = cCommon.ChatChannelWeb
	}

	// 5. 解析目标 Agent(请求体 agentId 优先,缺省取默认 Agent)
	agent, err := s.resolveAgent(comCode, req.AgentId)
	if err != nil {
		return nil, nil, dataobject.TLlm{}, err
	}

	// 6. 解析 LLM Provider(PrimaryModel 优先,Fallbacks 兜底)
	llmProvider, err := s.resolveProvider(comCode, agent)
	if err != nil {
		return nil, nil, dataobject.TLlm{}, err
	}

	// 7. 基于 Agent 工作区启动文档组装 system prompt
	skillFiles := s.collectSkillFiles(comCode, agent)
	systemPrompt, err := s.promptService.BuildSystemPromptEx(agent, skillFiles, dataobject.BootstrapScopeNormal)
	if err != nil {
		logging.LogErrorF("chat service: build system prompt error:%s", err.Error())
		return nil, nil, dataobject.TLlm{}, myException.ChatLLMErrEx2("chat service: build system prompt error:%s", err.Error())
	}

	return req, systemPrompt, llmProvider, nil
}

// ==================== private functions & methods ==========================

// llmError 统一转换 LLM 调用错误:客户端取消 → ChatCanceledErr(静默),其余 → ChatLLMErr。
func (s *ChatService) llmError(err error, logPrefix string) error {
	if errors.Is(err, myException.ErrRequestCanceled) {
		logging.LogDebugF("%s: request canceled by client", logPrefix)
		return myException.ChatCanceledErrEx("chat request canceled")
	}
	logging.LogErrorF("%s:%s", logPrefix, err.Error())
	return myException.ChatLLMErrEx2("%s:%s", logPrefix, err.Error())
}

// resolveAgent 解析本次会话的目标 Agent。
//   - agentId 非空:按 (comCode, agentId) 精确查找,未命中/被禁用返回 ChatAgentNotFoundErr
//   - agentId 为空:取 comCode 下默认 Agent(IsDefault),没有则取第一个可用 Agent
func (s *ChatService) resolveAgent(comCode, agentId string) (dataobject.TAgent, error) {
	if s.agentRepo == nil {
		return dataobject.TAgent{}, myException.ChatLLMErrEx("chat service: agent repository not initialized")
	}
	comCode = strings.TrimSpace(comCode)
	agentId = strings.TrimSpace(agentId)

	if agentId != "" {
		agent, ok := s.agentRepo.GetAgent(comCode, agentId)
		if !ok || agent.Deleted || agent.Disabled {
			return dataobject.TAgent{}, pException.UnMatchErrorEx2("chat agent not found: %s", agentId)
		}
		return agent, nil
	}

	agents := s.agentRepo.GetAgents(comCode)
	for i := range agents {
		if !agents[i].Deleted && !agents[i].Disabled && agents[i].IsDefault {
			return agents[i], nil
		}
	}
	for i := range agents {
		if !agents[i].Deleted && !agents[i].Disabled {
			return agents[i], nil
		}
	}
	return dataobject.TAgent{}, pException.UnMatchErrorEx2("no available chat agent for comCode:%s", comCode)
}

// resolveProvider 解析 Agent 使用的 LLM Provider:优先 PrimaryModel,再按 Fallbacks 逐个回退。
// 均不可用时返回 ChatAgentNotFoundErr(视为"目标 Agent 不可用")。
func (s *ChatService) resolveProvider(comCode string, agent dataobject.TAgent) (dataobject.TLlm, error) {
	if s.agentRepo == nil {
		return dataobject.TLlm{}, myException.ChatLLMErrEx("chat service: agent repository not initialized")
	}

	modelIds := make([]string, 0, 4)
	if id := strings.TrimSpace(agent.PrimaryModel); id != "" {
		modelIds = append(modelIds, id)
	}
	for _, fb := range strings.Split(agent.Fallbacks, ",") {
		if id := strings.TrimSpace(fb); id != "" {
			modelIds = append(modelIds, id)
		}
	}

	providers := s.agentRepo.GetLLMProviders(comCode)
	for _, modelId := range modelIds {
		for _, p := range providers {
			if p.ProviderId == modelId && !p.Disabled && !p.Deleted {
				return p, nil
			}
		}
	}

	return dataobject.TLlm{}, pException.UnMatchErrorEx2("no available llm provider for agent: %s", agent.AgentId)
}

// collectSkillFiles 收集该 comCode 下可用的 skill 文件路径。
// 相对路径显式拼接 Agent 工作区根(与 PromptRepository.resolveSkillPathLocked 语义一致);
// 文件缺失由仓库告警后跳过,不影响主流程。
func (s *ChatService) collectSkillFiles(comCode string, agent dataobject.TAgent) []string {
	if s.agentRepo == nil {
		return nil
	}

	skills := s.agentRepo.GetSkills(comCode)
	if len(skills) == 0 {
		return nil
	}

	workspaceRoot, _ := myUtility.BuildAgentBootstrapRoot(agent)
	files := make([]string, 0, len(skills))
	for i := range skills {
		dir := strings.TrimSpace(skills[i].SkillDir)
		if dir == "" {
			continue
		}
		if !filepath.IsAbs(dir) && workspaceRoot != "" {
			dir = filepath.Join(workspaceRoot, dir)
		}
		files = append(files, dir)
	}

	return files
}
