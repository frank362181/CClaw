/**
* Agent 管理业务服务
*
* 职责:管理 LLM Provider / Agent / Skill 三个核心对象
*   - 列表:读取仓库内存缓存(启动时从 DB 加载,CRUD 同步更新)
*   - 新增/更新/删除:先做业务校验,再经仓库落库并同步缓存
*   - 更新采用乐观锁(d_version):并发编辑时后提交者得到 409 冲突
*
* 与 LoginService 保持一致:组合 baseService.BaseService + 工厂方法 GetAgentService()。
**/

package service

import (
	"strings"

	myUtility "my.favorite/cclawservice/common"
	myException "my.favorite/cclawservice/exception"
	common2 "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/model/repo"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/database/core"
	"my.favorite/platformutility/exception"
	baseService "my.favorite/platformutility/webbase/middleware/service"
)

// AgentService 管理 LLM Provider / Agent / Skill
type AgentService struct {
	baseService.BaseService

	agentRepo       repo.IAgentRepository
	startupFileRepo repo.IAgentFileRepository
}

var agentService = common.GetSingleton(func() *AgentService {
	return &AgentService{
		BaseService: baseService.BaseService{
			Name:        "agentService",
			Description: "manage runtime providers, agents and skills",
		},
		agentRepo:       nil,
		startupFileRepo: nil,
	}
}).Instance()

// GetAgentService 构建 agent 管理服务(参数校验失败时返回 error,与 GetLoginService 一致)
func GetAgentService(db core.IBaseDatabase) (*AgentService, error) {
	if db == nil {
		return nil, exception.ParameterEmptyErrorEx("agent service: database is nil")
	}
	agentService.agentRepo = repo.GetAgentRepository(db)
	agentService.startupFileRepo = repo.GetAgentFileRepository()
	return agentService, nil
}

// ============================== LLM Provider =============================

func (s *AgentService) GetLLMProviders(comCode string) ([]dataobject.TLlm, error) {
	return s.agentRepo.GetLLMProviders(comCode), nil
}

func (s *AgentService) AddLLMProvider(p dataobject.TLlm) error {
	if err := myUtility.ValidateLLMProvider(p); err != nil {
		return err
	}
	myUtility.NormalizeOwner(&p.OwnerId)
	return s.agentRepo.AddLLMProvider(p)
}

func (s *AgentService) UpdateLLMProvider(p dataobject.TLlm) error {
	if err := myUtility.ValidateLLMProvider(p); err != nil {
		return err
	}
	myUtility.NormalizeOwner(&p.OwnerId)
	return s.agentRepo.UpdateLLMProvider(p)
}

func (s *AgentService) RemoveLLMProvider(ownerId, providerId string) error {
	ownerId = strings.TrimSpace(ownerId)
	providerId = strings.TrimSpace(providerId)
	if len(ownerId) == 0 || len(providerId) == 0 {
		return exception.ParameterEmptyErrorEx("provider id is empty")
	}
	return s.agentRepo.RemoveLLMProvider(ownerId, providerId)
}

// ============================== Agent =============================

func (s *AgentService) GetAgents(comCode string) ([]dataobject.TAgent, error) {
	return s.agentRepo.GetAgents(comCode), nil
}

func (s *AgentService) AddAgent(a dataobject.TAgent) error {
	if err := myUtility.ValidateAgent(a); err != nil {
		return err
	}
	myUtility.NormalizeOwner(&a.OwnerId)
	return s.agentRepo.AddAgent(a)
}

func (s *AgentService) UpdateAgent(a dataobject.TAgent) error {
	if err := myUtility.ValidateAgent(a); err != nil {
		return err
	}
	myUtility.NormalizeOwner(&a.OwnerId)
	return s.agentRepo.UpdateAgent(a)
}

func (s *AgentService) RemoveAgent(ownerId, agentId string) error {
	ownerId = strings.TrimSpace(ownerId)
	agentId = strings.TrimSpace(agentId)
	if len(ownerId) == 0 || len(agentId) == 0 {
		return exception.ParameterEmptyErrorEx("agent id is empty")
	}
	return s.agentRepo.RemoveAgent(ownerId, agentId)
}

// ============================== Agent Startup Files =============================

// GetAgentStartupFiles 获取某 agent 的启动文件清单(不含正文)。
// 先按 ownerId + agentId 校验归属,再从该 agent 的工作区读取启动文件元数据。
func (s *AgentService) GetAgentStartupFiles(ownerId, agentId string) ([]common2.AgentDocument, error) {
	ownerId = strings.TrimSpace(ownerId)
	agentId = strings.TrimSpace(agentId)
	if len(ownerId) == 0 || len(agentId) == 0 {
		return nil, exception.ParameterEmptyErrorEx("agent id is empty")
	}
	if err := s.requireRepos(); err != nil {
		return nil, err
	}

	workspaceRoot, err := s.resolveAgentWorkspace(ownerId, agentId)
	if err != nil {
		return nil, err
	}

	return s.startupFileRepo.GetStartupFiles(workspaceRoot), nil
}

// GetAgentStartupFile 获取某 agent 指定启动文件的原始内容。
// 文件必须命中 StartupDocFiles 白名单;文件缺失时返回 Missing=true 与空内容。
func (s *AgentService) GetAgentStartupFile(ownerId, agentId, fileName string) (*common2.AgentDocument, error) {
	ownerId = strings.TrimSpace(ownerId)
	agentId = strings.TrimSpace(agentId)
	fileName = strings.TrimSpace(fileName)
	if len(ownerId) == 0 || len(agentId) == 0 {
		return nil, exception.ParameterEmptyErrorEx("agent id is empty")
	}
	if len(fileName) == 0 {
		return nil, exception.ParameterEmptyErrorEx("startup file name is empty")
	}
	if err := s.requireRepos(); err != nil {
		return nil, err
	}

	workspaceRoot, err := s.resolveAgentWorkspace(ownerId, agentId)
	if err != nil {
		return nil, err
	}

	file, err := s.startupFileRepo.GetStartupFile(workspaceRoot, fileName)
	if err != nil {
		return nil, myException.MapStartupFileError(err)
	}
	return file, nil
}

// ============================== Skill =============================

func (s *AgentService) GetSkills(comCode string) ([]dataobject.TSkill, error) {
	return s.agentRepo.GetSkills(comCode), nil
}

func (s *AgentService) AddSkill(sk dataobject.TSkill) error {
	if err := myUtility.ValidateSkill(sk); err != nil {
		return err
	}
	myUtility.NormalizeOwner(&sk.OwnerId)
	return s.agentRepo.AddSkill(sk)
}

func (s *AgentService) UpdateSkill(sk dataobject.TSkill) error {
	if err := myUtility.ValidateSkill(sk); err != nil {
		return err
	}
	myUtility.NormalizeOwner(&sk.OwnerId)
	return s.agentRepo.UpdateSkill(sk)
}

func (s *AgentService) RemoveSkill(ownerId, skillId string) error {
	ownerId = strings.TrimSpace(ownerId)
	skillId = strings.TrimSpace(skillId)
	if len(ownerId) == 0 || len(skillId) == 0 {
		return exception.ParameterEmptyErrorEx("skill id is empty")
	}
	return s.agentRepo.RemoveSkill(ownerId, skillId)
}

// ==================== private functions & methods ==========================

// resolveAgentWorkspace 校验归属并解析 agent 的工作区根目录(启动文件两个接口共用的前置步骤)。
// agent 不存在/不属于该 owner → AgentNotFoundErr;工作区目录缺失/非法 → AgentWorkspaceErr。
func (s *AgentService) resolveAgentWorkspace(ownerId, agentId string) (string, error) {
	agent, err := s.getAgentByOwner(ownerId, agentId)
	if err != nil {
		return "", err
	}

	workspaceRoot, ok := common2.BuildAgentBootstrapRoot(*agent)
	if !ok {
		return "", myException.AgentWorkspaceErrEx("agent workspace is unavailable: " + agentId)
	}

	return workspaceRoot, nil
}

// getAgentByOwner 按归属 + 主键查找 agent,未找到或不属于该 owner 时返回业务错误。
func (s *AgentService) getAgentByOwner(ownerId, agentId string) (*dataobject.TAgent, error) {
	if s.agentRepo == nil {
		return nil, myException.AgentInternalErrEx("agent service: repository not initialized")
	}

	agent, ok := s.agentRepo.GetAgent(ownerId, agentId)
	if !ok {
		return nil, exception.UnMatchErrorEx2("agent not found: %s", agentId)
	}
	return &agent, nil
}

// requireRepos 校验服务依赖已注入,避免未调用 GetAgentService 时空指针 panic。
func (s *AgentService) requireRepos() error {
	if s.agentRepo == nil || s.startupFileRepo == nil {
		return myException.AgentInternalErrEx("agent service: repositories not initialized")
	}
	return nil
}
