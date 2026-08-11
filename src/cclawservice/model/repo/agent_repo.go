/**
* 所有Agent的配置信息仓库
**/

package repo

import (
	"my.favorite/cclawservice/model/dataaccessor"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/database/core"
)

type AgentRepository struct {
	dataAccessor dataaccessor.IAccessor
}

type IAgentRepository interface{}

var agentRepository = common.GetSingleton(func() *AgentRepository {
	return &AgentRepository{
		dataAccessor: nil,
	}
}).Instance()

func GetAgentRepository(db core.IBaseDatabase) IAgentRepository {
	agentRepository.dataAccessor, _ = dataaccessor.GetAccessor(db)
	return agentRepository
}
