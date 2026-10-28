/**
* 内容路由关键词规则数据对象
*
* 用于将用户消息按关键词路由到指定 Agent(方案C:关键词路由层),
* 通过 agent_id 与 t_agent 关联。关键词以逗号分隔,命中任一关键词即路由到目标 Agent。
**/

package dataobject

// TAgentRouteRule 一条内容路由规则:当用户消息命中任一关键词时,路由到指定 Agent。
// 同一 Agent 可配置多条规则(关键词分组);命中优先级按 Priority 从高到低(数值大者优先)。
type TAgentRouteRule struct {
	Id       int64  `gorm:"column:id;primaryKey" json:"Id"`
	OwnerId  string `gorm:"column:owner_id" json:"OwnerId"`  // 归属(与登录用户的 comCode 对齐)
	AgentId  string `gorm:"column:agent_id" json:"AgentId"`  // 目标 Agent(关联 t_agent.agent_id)
	Keywords string `gorm:"column:keywords" json:"Keywords"` // 逗号分隔的关键词
	Priority int    `gorm:"column:priority" json:"Priority"` // 命中优先级(数值大者优先)
	Disabled bool   `gorm:"column:disabled" json:"Disabled"` // 是否禁用
	Deleted  bool   `gorm:"column:deleted" json:"Deleted"`   // 是否删除(软删除)
}

// TableName 返回表名(gorm SingularTable 下保持单数,显式声明避免歧义)。
func (TAgentRouteRule) TableName() string {
	return "t_agent_route_rule"
}
