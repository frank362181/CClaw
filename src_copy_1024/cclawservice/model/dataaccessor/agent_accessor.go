package dataaccessor

import (
	"errors"

	"gorm.io/gorm"
	exception2 "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/database/core"
	"my.favorite/platformutility/exception"
)

type IAgentAccessor interface {
	GetDatabase() core.IBaseDatabase

	GetAllLLMProviders() ([]dataobject.TLlm, error)
	CreateLLMProvider(provider *dataobject.TLlm) error
	UpdateLLMProvider(provider *dataobject.TLlm) error
	DeleteLLMProvider(ownerId, providerId string) error

	GetAllAgentRouteRules() ([]dataobject.TAgentRouteRule, error)

	GetAllAgents() ([]dataobject.TAgent, error)
	CreateAgent(agent *dataobject.TAgent) error
	UpdateAgent(agent *dataobject.TAgent) error
	DeleteAgent(ownerId, agentId string) error

	GetSystemTools() ([]dataobject.TTool, error)
	CreateSystemTool(tool *dataobject.TTool) error
	UpdateSystemTool(tool *dataobject.TTool) error
	DeleteSystemTool(toolId string) error

	GetAllSkills() ([]dataobject.TSkill, error)
	CreateSkill(skill *dataobject.TSkill) error
	UpdateSkill(skill *dataobject.TSkill) error
	DeleteSkill(ownerId, skillId string) error
}

type AgentAccessor struct {
	BaseAccessor

	gormDb *gorm.DB
}

var agentAccessor = pCommon.GetSingleton(func() *AgentAccessor {
	return &AgentAccessor{
		BaseAccessor: BaseAccessor{
			db: nil,
		},
	}
}).Instance()

func GetAgentAccessor(db core.IBaseDatabase) (IAgentAccessor, error) {
	if db == nil {
		return nil, exception.ParameterEmptyErrorEx("agent accessor: database is nil")
	}

	gormDb, ok := db.(*core.GormDatabase)
	if !ok {
		return nil, exception.UnsupportedExceptionErrorEx2("agent accessor: database type[%s] mismatch", db.GetAccessorType())
	}

	agentAccessor.db = db
	agentAccessor.gormDb = gormDb.GetDB()
	return agentAccessor, nil
}

// ============================== 读取(仅未删除记录) =============================

func (g *AgentAccessor) GetAllLLMProviders() ([]dataobject.TLlm, error) {
	var list []dataobject.TLlm
	err := g.gormDb.Where("deleted = 0").Find(&list).Error
	if err != nil {
		return nil, exception2.DatabaseQueryErrEx2("load LLM providers error:%s", err.Error())
	}
	return list, nil
}

func (g *AgentAccessor) GetAllAgents() ([]dataobject.TAgent, error) {
	var list []dataobject.TAgent
	err := g.gormDb.Where("deleted = 0").Find(&list).Error
	if err != nil {
		return nil, exception2.DatabaseQueryErrEx2("load agents error:%s", err.Error())
	}

	//1. 若此时agent为空,就不需要从数据库中读取对应的skill了
	if len(list) == 0 {
		return list, nil
	}

	// 2. 用聚合查与agent关联的SKILL
	var agentIds []string
	for _, agent := range list {
		agentIds = append(agentIds, agent.AgentId)
	}

	type agentSkills struct {
		AgentId string `gorm:"agent_id"`
		SkillId string `gorm:"skill_id"`
	}

	var rows []agentSkills
	err = g.gormDb.Table("t_agent_skill").
		Select("agent_id,skill_id").
		Where("deleted = 0 and disabled = 0 and agent_id in ?", agentIds).
		Find(&rows).Error
	if err != nil {
		return nil, exception2.DatabaseQueryErrEx2("load agents error:%s", err.Error())
	}

	for index := range list {
		for _, tmp := range rows {
			if tmp.AgentId == list[index].AgentId {
				list[index].SkillIds = append(list[index].SkillIds, tmp.SkillId)
			}
		}
	}
	return list, nil
}

func (g *AgentAccessor) GetAllSkills() ([]dataobject.TSkill, error) {
	var list []dataobject.TSkill
	err := g.gormDb.Where("deleted = 0 and disabled = 0").Find(&list).Error
	if err != nil {
		return nil, exception2.DatabaseQueryErrEx2("load skills error:%s", err.Error())
	}
	return list, nil
}

func (g *AgentAccessor) GetAllAgentRouteRules() ([]dataobject.TAgentRouteRule, error) {
	var list []dataobject.TAgentRouteRule
	err := g.gormDb.Where("deleted = 0").Find(&list).Error
	if err != nil {
		return nil, exception2.DatabaseQueryErrEx2("load agent route rules error:%s", err.Error())
	}
	return list, nil
}

// ============================== LLM Provider 写操作 =============================

// CreateLLMProvider 新增 LLM Provider:
//   - 已存在活跃(未删除)记录 -> 冲突
//   - 存在软删除记录占用唯一键 -> 复活该记录(覆盖字段, deleted=0)
//   - 否则全新插入
func (g *AgentAccessor) CreateLLMProvider(p *dataobject.TLlm) error {
	var count int64
	if err := g.gormDb.Model(&dataobject.TLlm{}).
		Where("provider_id = ? AND deleted = 0", p.ProviderId).Count(&count).Error; err != nil {
		return exception2.DatabaseQueryErrEx2("check runtime provider error:%s", err.Error())
	}
	if count > 0 {
		return exception.MatchErrorEx2("runtime provider already exists:%s", p.ProviderId)
	}

	// 软删除记录占用唯一键时复活
	values := dataobject.LlmValues(p)
	values["deleted"] = 0
	values["d_version"] = gorm.Expr("d_version + 1")
	res := g.gormDb.Model(&dataobject.TLlm{}).
		Where("provider_id = ? AND deleted = 1", p.ProviderId).
		Updates(values)
	if res.Error != nil {
		return exception2.DatabaseQueryErrEx2("revive runtime provider error:%s", res.Error.Error())
	}
	if res.RowsAffected > 0 {
		// 复活成功:回读最新 d_version 写回内存结构,避免仓库缓存携带旧版本导致后续 Update 误报 409
		WriteBackRevivedVersion(g.gormDb, &dataobject.TLlm{}, "provider_id", p.ProviderId, &p.DVersion)
		return nil
	}

	// 全新插入。乐观锁插件 clause 会把 d_version 自动置为 1 并写回内存结构,
	// 因此这里无需预置 DVersion。
	if err := g.gormDb.Create(p).Error; err != nil {
		return exception2.DatabaseQueryErrEx2("create runtime provider error:%s", err.Error())
	}
	return nil
}

// UpdateLLMProvider 更新 LLM Provider(乐观锁:d_version 不匹配返回冲突)
func (g *AgentAccessor) UpdateLLMProvider(p *dataobject.TLlm) error {
	var cur dataobject.TLlm
	err := g.gormDb.Where("provider_id = ? AND owner_id = ? AND deleted = 0", p.ProviderId, p.OwnerId).First(&cur).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return exception.UnMatchErrorEx2("runtime provider not found: %s", p.ProviderId)
	}
	if err != nil {
		return exception2.DatabaseQueryErrEx2("load runtime provider error:%s", err.Error())
	}
	if cur.DVersion != p.DVersion {
		return exception.UnMatchErrorEx2("runtime provider version conflict:%s", p.ProviderId)
	}

	values := dataobject.LlmValues(p)
	values["d_version"] = gorm.Expr("d_version + 1")
	res := g.gormDb.Model(&dataobject.TLlm{}).
		Where("provider_id = ? AND owner_id = ? AND deleted = 0 AND d_version = ?", p.ProviderId, p.OwnerId, p.DVersion.Int64).
		Updates(values)
	if res.Error != nil {
		return exception2.DatabaseQueryErrEx2("update runtime provider error:%s", res.Error.Error())
	}
	if res.RowsAffected == 0 {
		return exception.MatchErrorEx2("runtime provider version conflict:%s", p.ProviderId)
	}

	p.DVersion.Int64++ // 同步内存版本,供仓库缓存更新
	return nil
}

// DeleteLLMProvider 软删除 LLM Provider
func (g *AgentAccessor) DeleteLLMProvider(ownerId, providerId string) error {
	res := g.gormDb.Model(&dataobject.TLlm{}).
		Where("provider_id = ? AND owner_id = ? AND deleted = 0", providerId, ownerId).
		Update("deleted", 1)
	if res.Error != nil {
		return exception2.DatabaseQueryErrEx2("delete runtime provider error:%s", res.Error.Error())
	}
	if res.RowsAffected == 0 {
		return exception.UnMatchErrorEx2("runtime provider not found:%s", providerId)
	}
	return nil
}

// ============================== Agent 写操作 =============================

func (g *AgentAccessor) CreateAgent(a *dataobject.TAgent) error {
	var count int64
	if err := g.gormDb.Model(&dataobject.TAgent{}).
		Where("agent_id = ? AND deleted = 0", a.AgentId).Count(&count).Error; err != nil {
		return exception2.DatabaseQueryErrEx2("check agent error:%s", err.Error())
	}
	if count > 0 {
		return exception.MatchErrorEx2("agent already exists:%s", a.AgentName)
	}

	values := dataobject.AgentValues(a)
	values["deleted"] = 0
	values["d_version"] = gorm.Expr("d_version + 1")
	res := g.gormDb.Model(&dataobject.TAgent{}).
		Where("agent_id = ? AND deleted = 1", a.AgentId).
		Updates(values)
	if res.Error != nil {
		return exception2.DatabaseQueryErrEx2("revive agent error:%s", res.Error.Error())
	}
	if res.RowsAffected > 0 {
		// 复活成功:回读最新 d_version 写回内存结构,避免仓库缓存携带旧版本导致后续 Update 误报 409
		WriteBackRevivedVersion(g.gormDb, &dataobject.TAgent{}, "agent_id", a.AgentId, &a.DVersion)
		return nil
	}

	// 乐观锁插件 clause 自动将 d_version 置为 1,无需预置
	if err := g.gormDb.Create(a).Error; err != nil {
		return exception2.DatabaseQueryErrEx2("create agent error:%s", err.Error())
	}
	return nil
}
func (g *AgentAccessor) UpdateAgent(a *dataobject.TAgent) error {
	var cur dataobject.TAgent
	err := g.gormDb.Where("agent_id = ? AND owner_id = ? AND deleted = 0", a.AgentId, a.OwnerId).First(&cur).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return exception.UnMatchErrorEx2("agent not found:%s", a.AgentId)
	}
	if err != nil {
		return exception2.DatabaseQueryErrEx2("load agent error:%s", err.Error())
	}
	if cur.DVersion != a.DVersion {
		return exception.UnMatchErrorEx2("agent version conflict:%s", a.AgentId)
	}

	values := dataobject.AgentValues(a)
	values["d_version"] = gorm.Expr("d_version + 1")
	res := g.gormDb.Model(&dataobject.TAgent{}).
		Where("agent_id = ? AND owner_id = ? AND deleted = 0 AND d_version = ?", a.AgentId, a.OwnerId, a.DVersion.Int64).
		Updates(values)
	if res.Error != nil {
		return exception2.DatabaseQueryErrEx2("update agent error:%s", res.Error.Error())
	}
	if res.RowsAffected == 0 {
		return exception.UnMatchErrorEx2("agent version conflict:%s", a.AgentId)
	}

	a.DVersion.Int64++
	return nil
}

func (g *AgentAccessor) DeleteAgent(ownerId, agentId string) error {
	res := g.gormDb.Model(&dataobject.TAgent{}).
		Where("agent_id = ? AND owner_id =? AND deleted = 0", agentId, ownerId).
		Update("deleted", 1)
	if res.Error != nil {
		return exception2.DatabaseQueryErrEx2("delete agent error:%s", res.Error.Error())
	}
	if res.RowsAffected == 0 {
		return exception.UnMatchErrorEx2("agent not found: %s", agentId)
	}
	return nil
}

// ============================== base tool 管理操作 ========================

func (g *AgentAccessor) GetSystemTools() ([]dataobject.TTool, error) {
	var tools []dataobject.TTool
	err := g.gormDb.Where("disabled = 0 and deleted = 0").Find(&tools).Error
	if err != nil {
		return nil, exception2.DatabaseQueryErrEx2("get tools error:%s", err.Error())
	}

	return tools, nil
}

func (g *AgentAccessor) CreateSystemTool(tool *dataobject.TTool) error {
	var count int64
	if err := g.gormDb.Model(&dataobject.TTool{}).
		Where("tool_id = ? AND deleted = 0", tool.ToolId).Count(&count).Error; err != nil {
		return exception2.DatabaseQueryErrEx2("create tool error:%s", err.Error())
	}

	if count > 0 {
		return exception.MatchErrorEx2("tool already exists:%s", tool.ToolName)
	}

	values := dataobject.ToolValues(tool)
	values["deleted"] = 0
	values["d_version"] = gorm.Expr("d_version + 1")
	res := g.gormDb.Model(&dataobject.TTool{}).
		Where("tool_id = ? and deleted = 1", tool.ToolId).
		Updates(values)
	if res.Error != nil {
		WriteBackRevivedVersion(g.gormDb, &dataobject.TTool{}, "tool_id", tool.ToolId, &tool.DVersion)
		return exception2.DatabaseQueryErrEx2("revive tool error:%s", res.Error.Error())
	}

	if res.RowsAffected > 0 {
		return nil
	}

	//乐观锁 clause自动将 d_version 设置为1
	if err := g.gormDb.Create(tool).Error; err != nil {
		return exception2.DatabaseQueryErrEx2("create tool error:%s", err.Error())
	}
	return nil
}

func (g *AgentAccessor) UpdateSystemTool(tool *dataobject.TTool) error {
	var curTool dataobject.TTool
	err := g.gormDb.Where("tool_id = ? and deleted = 0", tool.ToolId).First(&curTool).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return exception.UnMatchErrorEx2("tool not found:%s", tool.ToolName)
	}

	if err != nil {
		return exception2.DatabaseQueryErrEx2("update tool error:%s", err.Error())
	}

	if curTool.DVersion != tool.DVersion {
		return exception.UnMatchErrorEx2("tool version conflict:%s", tool.ToolId)
	}

	values := dataobject.ToolValues(tool)
	values["d_version"] = gorm.Expr("d_version + 1")
	res := g.gormDb.Model(&dataobject.TTool{}).
		Where("tool_id = ? and deleted = 0 and d_version = ?", tool.ToolId, tool.DVersion.Int64).
		Updates(values)
	if res.Error != nil {
		return exception2.DatabaseQueryErrEx2("update tool error:%s", res.Error.Error())
	}

	if res.RowsAffected == 0 {
		return exception.UnMatchErrorEx2("tool version conflict:%s", tool.ToolId)
	}
	tool.DVersion.Int64++
	return nil
}

func (g *AgentAccessor) DeleteSystemTool(toolId string) error {
	res := g.gormDb.Model(&dataobject.TTool{}).
		Where("tool_id = ? AND deleted = 0", toolId).
		Update("deleted", 1)

	if res.Error != nil {
		return exception2.DatabaseQueryErrEx2("delete tool error:%s", res.Error.Error())
	}

	if res.RowsAffected == 0 {
		return exception.UnMatchErrorEx2("tool not found:%s", toolId)
	}
	return nil
}

// ============================== Skill 写操作 =============================

func (g *AgentAccessor) CreateSkill(sk *dataobject.TSkill) error {
	var count int64
	if err := g.gormDb.Model(&dataobject.TSkill{}).
		Where("skill_id = ? AND deleted = 0", sk.SkillId).Count(&count).Error; err != nil {
		return exception2.DatabaseQueryErrEx2("check skill error:%s", err.Error())
	}
	if count > 0 {
		return exception.MatchErrorEx2("skill already exists:%s", sk.SkillId)
	}

	values := dataobject.SkillValues(sk)
	values["deleted"] = 0
	values["d_version"] = gorm.Expr("d_version + 1")
	res := g.gormDb.Model(&dataobject.TSkill{}).
		Where("skill_id = ? AND deleted = 1", sk.SkillId).
		Updates(values)
	if res.Error != nil {
		return exception2.DatabaseQueryErrEx2("revive skill error:%s", res.Error.Error())
	}
	if res.RowsAffected > 0 {
		// 复活成功:回读最新 d_version 写回内存结构,避免仓库缓存携带旧版本导致后续 Update 误报 409
		WriteBackRevivedVersion(g.gormDb, &dataobject.TSkill{}, "skill_id", sk.SkillId, &sk.DVersion)
		return nil
	}

	// 乐观锁插件 clause 自动将 d_version 置为 1,无需预置
	if err := g.gormDb.Create(sk).Error; err != nil {
		return exception2.DatabaseQueryErrEx2("create skill error:%s", err.Error())
	}
	return nil
}

func (g *AgentAccessor) UpdateSkill(sk *dataobject.TSkill) error {
	var cur dataobject.TSkill
	err := g.gormDb.Where("skill_id = ? AND owner_id = ? AND deleted = 0", sk.SkillId, sk.OwnerId).First(&cur).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return exception.UnMatchErrorEx2("skill not found: %s", sk.SkillId)
	}
	if err != nil {
		return exception2.DatabaseQueryErrEx2("load skill error:%s", err.Error())
	}
	if cur.DVersion != sk.DVersion {
		return exception.UnMatchErrorEx2("skill version conflict:%s", sk.SkillId)
	}

	values := dataobject.SkillValues(sk)
	values["d_version"] = gorm.Expr("d_version + 1")
	res := g.gormDb.Model(&dataobject.TSkill{}).
		Where("skill_id = ? AND owner_id = ? AND deleted = 0 AND d_version = ?", sk.SkillId, sk.OwnerId, sk.DVersion.Int64).
		Updates(values)
	if res.Error != nil {
		return exception2.DatabaseQueryErrEx2("update skill error:%s", res.Error.Error())
	}
	if res.RowsAffected == 0 {
		return exception.UnMatchErrorEx2("skill version conflict:%s", sk.SkillId)
	}

	sk.DVersion.Int64++
	return nil
}

func (g *AgentAccessor) DeleteSkill(ownerId, skillId string) error {
	res := g.gormDb.Model(&dataobject.TSkill{}).
		Where("skill_id = ? AND owner_id = ? AND deleted = 0", skillId, ownerId).
		Update("deleted", 1)
	if res.Error != nil {
		return exception2.DatabaseQueryErrEx2("delete skill error:%s", res.Error.Error())
	}
	if res.RowsAffected == 0 {
		return exception.UnMatchErrorEx2("skill not found: %s", skillId)
	}
	return nil
}
