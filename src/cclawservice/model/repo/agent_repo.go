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
