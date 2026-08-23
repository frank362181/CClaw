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

	myException "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/model/repo"
	myUtility "my.favorite/cclawservice/utility"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/database/core"
	"my.favorite/platformutility/exception"
	baseService "my.favorite/platformutility/webbase/middleware/service"
)

// AgentService 管理 LLM Provider / Agent / Skill
type AgentService struct {
	baseService.BaseService

	agentRepo repo.IAgentRepository
}

var agentService = common.GetSingleton(func() *AgentService {
	return &AgentService{
		BaseService: baseService.BaseService{
			Name:        "agentService",
			Description: "manage llm providers, agents and skills",
		},
		agentRepo: nil,
	}
}).Instance()

// GetAgentService 构建 agent 管理服务(参数校验失败时返回 error,与 GetLoginService 一致)
func GetAgentService(db core.IBaseDatabase) (*AgentService, error) {
	if db == nil {
		return nil, exception.ParameterEmptyErrorEx("agent service: database is nil")
	}
	agentService.agentRepo = repo.GetAgentRepository(db)
	return agentService, nil
}

// ============================== LLM Provider =============================

func (s *AgentService) GetLLMProviders(comCode string) ([]dataobject.TLLMProvider, error) {
	return s.agentRepo.GetLLMProviders(comCode), nil
}

func (s *AgentService) AddLLMProvider(p dataobject.TLLMProvider) error {
	if err := myUtility.ValidateLLMProvider(p); err != nil {
		return err
	}
	myUtility.NormalizeOwner(&p.OwnerId)
	return s.agentRepo.AddLLMProvider(p)
}

func (s *AgentService) UpdateLLMProvider(p dataobject.TLLMProvider) error {
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
		return myException.AgentParameterErrEx("provider id is empty")
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
		return myException.AgentParameterErrEx("agent id is empty")
	}
	return s.agentRepo.RemoveAgent(ownerId, agentId)
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
		return myException.AgentParameterErrEx("skill id is empty")
	}
	return s.agentRepo.RemoveSkill(ownerId, skillId)
}
