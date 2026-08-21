/**
* 从数据库中获取所有的llm providers,agent,skill等信息
* 这里并不会加载agent的启动文件以及SKILL.md等文件内容
**/

package repo

import (
	"my.favorite/cclawservice/model/dataaccessor"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/database/core"
)

type AgentRepository struct {
	dataAccessor dataaccessor.IAccessor

	//llm provider list
	llmProviders *common.SafeList[dataobject.TLLMProvider]

	agents *common.SafeList[dataobject.TAgent]
	skills *common.SafeList[dataobject.TSkill]
}

type IAgentRepository interface {
	LoadAllAgents()

	GetAllLLMProviders() []dataobject.TLLMProvider
	AddLLMProvider(provider dataobject.TLLMProvider)
	RemoveLLMProvider(providerId string)

	GetAllAgents() []dataobject.TAgent
	AddAgent(agent dataobject.TAgent)
	RemoveAgent(agentId string)

	GetAllSkills() []dataobject.TSkill
	AddSkill(skill dataobject.TSkill)
	RemoveSkill(skillId string)

	Destroy()
}

var agentRepository = common.GetSingleton(func() *AgentRepository {
	return &AgentRepository{
		dataAccessor: nil,

		llmProviders: common.NewSafeList[dataobject.TLLMProvider](),
		agents:       common.NewSafeList[dataobject.TAgent](),
		skills:       common.NewSafeList[dataobject.TSkill](),
	}
}).Instance()

func GetAgentRepository(db core.IBaseDatabase) IAgentRepository {
	agentRepository.dataAccessor, _ = dataaccessor.GetAccessor(db)
	return agentRepository
}

func (repo *AgentRepository) LoadAllAgents() {
	repo.loadLLMProviders()
	repo.loadAgents()
	repo.loadSkills()
}

func (repo *AgentRepository) GetAllLLMProviders() []dataobject.TLLMProvider {
	out := make([]dataobject.TLLMProvider, 0, repo.llmProviders.Len())
	repo.llmProviders.Range(func(_ int, item dataobject.TLLMProvider) bool {
		out = append(out, item)
		return true
	})

	return out
}

func (repo *AgentRepository) AddLLMProvider(provider dataobject.TLLMProvider) {

}

func (repo *AgentRepository) RemoveLLMProvider(providerId string) {

}

func (repo *AgentRepository) GetAllAgents() []dataobject.TAgent {
	out := make([]dataobject.TAgent, 0, repo.agents.Len())
	repo.agents.Range(func(_ int, item dataobject.TAgent) bool {
		out = append(out, item)
		return true
	})

	return out
}

func (repo *AgentRepository) AddAgent(agent dataobject.TAgent) {

}

func (repo *AgentRepository) RemoveAgent(agentId string) {}

func (repo *AgentRepository) GetAllSkills() []dataobject.TSkill {
	out := make([]dataobject.TSkill, 0, repo.agents.Len())
	repo.skills.Range(func(_ int, item dataobject.TSkill) bool {
		out = append(out, item)
		return true
	})

	return out
}

func (repo *AgentRepository) AddSkill(skill dataobject.TSkill) {

}

func (repo *AgentRepository) RemoveSkill(skillId string) {}

func (repo *AgentRepository) Destroy() {
	if repo.llmProviders != nil {
		repo.llmProviders.Clear()
	}

	if repo.agents != nil {
		repo.agents.Clear()
	}

	if repo.skills != nil {
		repo.skills.Clear()
	}
}

// ============================== private functions & methods =========================

func (repo *AgentRepository) loadLLMProviders() {
	var providers []dataobject.TLLMProvider
	err := repo.dataAccessor.GetAllLLMProviders(&providers)
	if err == nil {
		for _, p := range providers {
			repo.llmProviders.Append(p)
		}
	}
}

func (repo *AgentRepository) loadAgents() {
	var agents []dataobject.TAgent
	err := repo.dataAccessor.GetAllAgents(&agents)
	if err != nil {
		for _, a := range agents {
			repo.agents.Append(a)
		}
	}
}

func (repo *AgentRepository) loadSkills() {
	var skills []dataobject.TSkill
	err := repo.dataAccessor.GetAllSkills(&skills)
	if err == nil {
		for _, s := range skills {
			repo.skills.Append(s)
		}
	}
}
