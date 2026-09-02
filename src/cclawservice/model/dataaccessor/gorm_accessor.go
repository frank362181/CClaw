package dataaccessor

import (
	"errors"
	"strings"

	"gorm.io/gorm"
	exception2 "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/common/hash"
	"my.favorite/platformutility/database/core"
	"my.favorite/platformutility/exception"
)

type GormAccessor struct {
	BaseAccessor

	gormDb *gorm.DB
}

var gormAccessor = pCommon.GetSingleton(func() *GormAccessor {
	return &GormAccessor{
		BaseAccessor: BaseAccessor{
			db: nil,
		},
	}
}).Instance()

func GetGormAccessor(db core.IBaseDatabase) IAccessor {
	gormAccessor.db = db

	//仅仅支持gorm实现
	gormDb, ok := gormAccessor.db.(*core.GormDatabase)
	if !ok {
		return nil
	}

	gormAccessor.gormDb = gormDb.GetDB()
	return gormAccessor
}

func (g *GormAccessor) GetUserByNameAndPassword(username, userpass string) (*dataobject.TAccount, error) {
	username = strings.ToLower(username)
	username = strings.TrimSpace(username)
	if len(username) == 0 {
		return nil, exception.ParameterEmptyErrorEx("username must not be empty")
	}

	var user dataobject.TAccount
	err := g.gormDb.First(&user, "login_name = ?", username).Error
	if err != nil {
		//找不到用户
		return nil, exception.UnMatchErrorEx2("username=%s can not find", username)
	}

	encryptedPass := hash.HmacSha256(user.LoginKey, userpass)
	// 恒定时间比较,避免时序侧信道泄露 LoginKey/LoginPass 信息
	if hash.HmacEqual([]byte(encryptedPass), []byte(user.LoginPass)) {
		return &user, nil
	}

	return nil, exception2.UserPasswordUnmatchEx("username and password do not match")
}

func (g *GormAccessor) GetUserByAccount(username string) (*dataobject.TAccount, error) {
	username = strings.ToLower(username)
	username = strings.TrimSpace(username)
	if len(username) == 0 {
		return nil, exception.ParameterEmptyErrorEx("username must not be empty")
	}

	var user dataobject.TAccount
	err := g.gormDb.First(&user, "login_name = ?", username).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil // 未命中,约定返回(nil,nil),由调用方判空
	}

	if err != nil {
		return nil, err // 真实db错误透传
	}

	return &user, nil
}

// ============================== 读取(仅未删除记录) =============================

func (g *GormAccessor) GetAllLLMProviders() ([]dataobject.TLlm, error) {
	var list []dataobject.TLlm
	err := g.gormDb.Where("deleted = 0").Find(&list).Error
	if err != nil {
		return nil, exception2.DatabaseQueryErrEx2("load LLM providers error:%s", err.Error())
	}
	return list, nil
}

func (g *GormAccessor) GetAllAgents() ([]dataobject.TAgent, error) {
	var list []dataobject.TAgent
	err := g.gormDb.Where("deleted = 0").Find(&list).Error
	if err != nil {
		return nil, exception2.DatabaseQueryErrEx2("load agents error:%s", err.Error())
	}
	return list, nil
}

func (g *GormAccessor) GetAllSkills() ([]dataobject.TSkill, error) {
	var list []dataobject.TSkill
	err := g.gormDb.Where("deleted = 0").Find(&list).Error
	if err != nil {
		return nil, exception2.DatabaseQueryErrEx2("load skills error:%s", err.Error())
	}
	return list, nil
}

func (g *GormAccessor) GetAllAgentRouteRules() ([]dataobject.TAgentRouteRule, error) {
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
func (g *GormAccessor) CreateLLMProvider(p *dataobject.TLlm) error {
	var count int64
	if err := g.gormDb.Model(&dataobject.TLlm{}).
		Where("provider_id = ? AND deleted = 0", p.ProviderId).Count(&count).Error; err != nil {
		return exception2.DatabaseQueryErrEx2("check runtime provider error:%s", err.Error())
	}
	if count > 0 {
		return exception2.AgentConflictErrEx2("runtime provider already exists:%s", p.ProviderId)
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
		WriteBackRevivedVersion(g, &dataobject.TLlm{}, "provider_id", p.ProviderId, &p.DVersion)
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
func (g *GormAccessor) UpdateLLMProvider(p *dataobject.TLlm) error {
	var cur dataobject.TLlm
	err := g.gormDb.Where("provider_id = ? AND owner_id = ? AND deleted = 0", p.ProviderId, p.OwnerId).First(&cur).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return exception2.AgentNotFoundErrEx("runtime provider not found:" + p.ProviderId)
	}
	if err != nil {
		return exception2.DatabaseQueryErrEx2("load runtime provider error:%s", err.Error())
	}
	if cur.DVersion != p.DVersion {
		return exception2.AgentConflictErrEx2("runtime provider version conflict:%s", p.ProviderId)
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
		return exception2.AgentConflictErrEx2("runtime provider version conflict:%s", p.ProviderId)
	}

	p.DVersion.Int64++ // 同步内存版本,供仓库缓存更新
	return nil
}

// DeleteLLMProvider 软删除 LLM Provider
func (g *GormAccessor) DeleteLLMProvider(ownerId, providerId string) error {
	res := g.gormDb.Model(&dataobject.TLlm{}).
		Where("provider_id = ? AND owner_id = ? AND deleted = 0", providerId, ownerId).
		Update("deleted", 1)
	if res.Error != nil {
		return exception2.DatabaseQueryErrEx2("delete runtime provider error:%s", res.Error.Error())
	}
	if res.RowsAffected == 0 {
		return exception2.AgentNotFoundErrEx("runtime provider not found:" + providerId)
	}
	return nil
}

// ============================== Agent 写操作 =============================

func (g *GormAccessor) CreateAgent(a *dataobject.TAgent) error {
	var count int64
	if err := g.gormDb.Model(&dataobject.TAgent{}).
		Where("agent_id = ? AND deleted = 0", a.AgentId).Count(&count).Error; err != nil {
		return exception2.DatabaseQueryErrEx2("check agent error:%s", err.Error())
	}
	if count > 0 {
		return exception2.AgentConflictErrEx2("agent already exists:%s", a.AgentId)
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
		WriteBackRevivedVersion(g, &dataobject.TAgent{}, "agent_id", a.AgentId, &a.DVersion)
		return nil
	}

	// 乐观锁插件 clause 自动将 d_version 置为 1,无需预置
	if err := g.gormDb.Create(a).Error; err != nil {
		return exception2.DatabaseQueryErrEx2("create agent error:%s", err.Error())
	}
	return nil
}
func (g *GormAccessor) UpdateAgent(a *dataobject.TAgent) error {
	var cur dataobject.TAgent
	err := g.gormDb.Where("agent_id = ? AND owner_id = ? AND deleted = 0", a.AgentId, a.OwnerId).First(&cur).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return exception2.AgentNotFoundErrEx("agent not found:" + a.AgentId)
	}
	if err != nil {
		return exception2.DatabaseQueryErrEx2("load agent error:%s", err.Error())
	}
	if cur.DVersion != a.DVersion {
		return exception2.AgentConflictErrEx2("agent version conflict:%s", a.AgentId)
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
		return exception2.AgentConflictErrEx2("agent version conflict:%s", a.AgentId)
	}

	a.DVersion.Int64++
	return nil
}

func (g *GormAccessor) DeleteAgent(ownerId, agentId string) error {
	res := g.gormDb.Model(&dataobject.TAgent{}).
		Where("agent_id = ? AND owner_id =? AND deleted = 0", agentId, ownerId).
		Update("deleted", 1)
	if res.Error != nil {
		return exception2.DatabaseQueryErrEx2("delete agent error:%s", res.Error.Error())
	}
	if res.RowsAffected == 0 {
		return exception2.AgentNotFoundErrEx("agent not found:" + agentId)
	}
	return nil
}

// ============================== Skill 写操作 =============================

func (g *GormAccessor) CreateSkill(sk *dataobject.TSkill) error {
	var count int64
	if err := g.gormDb.Model(&dataobject.TSkill{}).
		Where("skill_id = ? AND deleted = 0", sk.SkillId).Count(&count).Error; err != nil {
		return exception2.DatabaseQueryErrEx2("check skill error:%s", err.Error())
	}
	if count > 0 {
		return exception2.AgentConflictErrEx2("skill already exists:%s", sk.SkillId)
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
		WriteBackRevivedVersion(g, &dataobject.TSkill{}, "skill_id", sk.SkillId, &sk.DVersion)
		return nil
	}

	// 乐观锁插件 clause 自动将 d_version 置为 1,无需预置
	if err := g.gormDb.Create(sk).Error; err != nil {
		return exception2.DatabaseQueryErrEx2("create skill error:%s", err.Error())
	}
	return nil
}

func (g *GormAccessor) UpdateSkill(sk *dataobject.TSkill) error {
	var cur dataobject.TSkill
	err := g.gormDb.Where("skill_id = ? AND owner_id = ? AND deleted = 0", sk.SkillId, sk.OwnerId).First(&cur).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return exception2.AgentNotFoundErrEx("skill not found:" + sk.SkillId)
	}
	if err != nil {
		return exception2.DatabaseQueryErrEx2("load skill error:%s", err.Error())
	}
	if cur.DVersion != sk.DVersion {
		return exception2.AgentConflictErrEx2("skill version conflict:%s", sk.SkillId)
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
		return exception2.AgentConflictErrEx2("skill version conflict:%s", sk.SkillId)
	}

	sk.DVersion.Int64++
	return nil
}

func (g *GormAccessor) DeleteSkill(ownerId, skillId string) error {
	res := g.gormDb.Model(&dataobject.TSkill{}).
		Where("skill_id = ? AND owner_id = ? AND deleted = 0", skillId, ownerId).
		Update("deleted", 1)
	if res.Error != nil {
		return exception2.DatabaseQueryErrEx2("delete skill error:%s", res.Error.Error())
	}
	if res.RowsAffected == 0 {
		return exception2.AgentNotFoundErrEx("skill not found:" + skillId)
	}
	return nil
}
