package model

// ================ agent 管理请求模型 ================

// LLMProviderIDRequest 按业务主键操作 LLM Provider(如删除)
type LLMProviderIDRequest struct {
	ProviderId string `json:"providerId" binding:"required"`
}

// AgentIDRequest 按业务主键操作 Agent(如删除)
type AgentIDRequest struct {
	AgentId string `json:"agentId" binding:"required"`
}

// SkillIDRequest 按业务主键操作 Skill(如删除)
type SkillIDRequest struct {
	SkillId string `json:"skillId" binding:"required"`
}
