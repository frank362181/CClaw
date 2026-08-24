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

// AgentStartupFileRequest 按业务主键 + 文件名读取 agent 启动文件
type AgentStartupFileRequest struct {
	AgentId  string `json:"agentId" binding:"required"`
	FileName string `json:"fileName" binding:"required"`
}

// SkillIDRequest 按业务主键操作 Skill(如删除)
type SkillIDRequest struct {
	SkillId string `json:"skillId" binding:"required"`
}
