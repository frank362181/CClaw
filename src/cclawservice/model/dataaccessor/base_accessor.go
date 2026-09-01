package dataaccessor

import (
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/platformutility/database/core"
)

type BaseAccessor struct {
	db core.IBaseDatabase
}

type IAccessor interface {
	GetDatabase() core.IBaseDatabase
	GetUserByNameAndPassword(username, userpass string) (*dataobject.TAccount, error)
	GetUserByAccount(username string) (*dataobject.TAccount, error)

	// 读取(仅未删除记录)
	GetAllLLMProviders() ([]dataobject.TLlm, error)
	GetAllAgents() ([]dataobject.TAgent, error)
	GetAllSkills() ([]dataobject.TSkill, error)
	GetAllAgentRouteRules() ([]dataobject.TAgentRouteRule, error)

	// 写操作(新增/更新/软删除)
	CreateLLMProvider(provider *dataobject.TLlm) error
	UpdateLLMProvider(provider *dataobject.TLlm) error
	DeleteLLMProvider(ownerId, providerId string) error

	CreateAgent(agent *dataobject.TAgent) error
	UpdateAgent(agent *dataobject.TAgent) error
	DeleteAgent(ownerId, agentId string) error

	CreateSkill(skill *dataobject.TSkill) error
	UpdateSkill(skill *dataobject.TSkill) error
	DeleteSkill(ownerId, skillId string) error
}

func (accessor *BaseAccessor) GetDatabase() core.IBaseDatabase {
	return accessor.db
}
