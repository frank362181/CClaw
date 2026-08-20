/**
* 所有Agent的配置信息仓库
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
