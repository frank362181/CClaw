/**
* 从数据库中获取所有的llm providers,agent,skill等信息
* 这里并不会加载agent的启动文件以及SKILL.md等文件内容
**/

package repo

import (
	"math"
	"strings"
	"sync"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/model/dataaccessor"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/database/core"
	"my.favorite/platformutility/exception"
)

// AgentRepository 持有 LLM Provider / Agent / Skill 的内存缓存,并负责与 DB 保持一致。
// 并发安全:列表读取使用 SafeList(读锁);写操作通过 mu 串行化"DB + 缓存"的变更。
// 多实例一致性:缓存带 TTL,读取前经 refreshIfExpired 判断是否过期,过期则从 DB 全量重载,
// 使共享同一 DB 的其他实例能及时应用管理员在别处做的变更(方案A)。
type AgentRepository struct {
	mu sync.Mutex

	dataAccessor dataaccessor.IAccessor

	llmProviders *common.SafeList[dataobject.TLLMProvider]
	agents       *common.SafeList[dataobject.TAgent]
	skills       *common.SafeList[dataobject.TSkill]

	// 配置缓存 TTL 刷新
	configTTL  int64 // 缓存过期时间;<=0 表示禁用 TTL 刷新(仅启动时加载一次)
	lastLoaded int64 //time.Time     // 最近一次从 DB 全量加载的时间
	refreshing bool  // 是否正在重载(防止并发读造成重载风暴)
}

type IAgentRepository interface {
	LoadAllAgents(ttl int64) []dataobject.TAgent

	GetLLMProviders(comCode string) []dataobject.TLLMProvider
	AddLLMProvider(provider dataobject.TLLMProvider) error
	UpdateLLMProvider(provider dataobject.TLLMProvider) error
	RemoveLLMProvider(providerId string) error

	GetAgents(comCode string) []dataobject.TAgent
	AddAgent(agent dataobject.TAgent) error
	UpdateAgent(agent dataobject.TAgent) error
	RemoveAgent(agentId string) error

	GetSkills(comCode string) []dataobject.TSkill
	AddSkill(skill dataobject.TSkill) error
	UpdateSkill(skill dataobject.TSkill) error
	RemoveSkill(skillId string) error

	Destroy()
}

var agentRepository = common.GetSingleton(func() *AgentRepository {
	return &AgentRepository{
		dataAccessor: nil,
		configTTL:    cCommon.DefaultReloadDatabaseCacheTTLSeconds,

		llmProviders: common.NewSafeList[dataobject.TLLMProvider](),
		agents:       common.NewSafeList[dataobject.TAgent](),
		skills:       common.NewSafeList[dataobject.TSkill](),
	}
}).Instance()

func GetAgentRepository(db core.IBaseDatabase) IAgentRepository {
	agentRepository.dataAccessor, _ = dataaccessor.GetAccessor(db)
	return agentRepository
}

func (repo *AgentRepository) LoadAllAgents(ttl int64) []dataobject.TAgent {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	repo.configTTL = int64(math.Max(0, float64(ttl)))
	repo.loadAllAgents()

	out := make([]dataobject.TAgent, 0, repo.agents.Len())
	repo.agents.Range(func(_ int, item dataobject.TAgent) bool {
		out = append(out, item)
		return true
	})

	return out
}

func (repo *AgentRepository) GetLLMProviders(comCode string) []dataobject.TLLMProvider {
	repo.refreshIfExpired()

	out := make([]dataobject.TLLMProvider, 0, repo.llmProviders.Len())
	repo.llmProviders.Range(func(_ int, item dataobject.TLLMProvider) bool {
		if strings.EqualFold(comCode, item.OwnerId) {
			out = append(out, item)
		}
		return true
	})

	return out
}

func (repo *AgentRepository) AddLLMProvider(provider dataobject.TLLMProvider) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	if err := repo.requireAccessor(); err != nil {
		return err
	}
	if err := repo.dataAccessor.CreateLLMProvider(&provider); err != nil {
		return err
	}
	repo.llmProviders.Append(provider)
	repo.lastLoaded = common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp //time.Now() // 本地缓存已与 DB 同步,重置 TTL 计时
	return nil
}

func (repo *AgentRepository) UpdateLLMProvider(provider dataobject.TLLMProvider) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	if err := repo.requireAccessor(); err != nil {
		return err
	}
	if err := repo.dataAccessor.UpdateLLMProvider(&provider); err != nil {
		return err
	}
	idx := common.FindIndex(repo.llmProviders, func(p dataobject.TLLMProvider) bool {
		return p.ProviderId == provider.ProviderId
	})
	if idx >= 0 {
		_ = repo.llmProviders.Update(idx, provider)
	}
	repo.lastLoaded = common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp //time.Now()
	return nil
}

func (repo *AgentRepository) RemoveLLMProvider(providerId string) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	if err := repo.requireAccessor(); err != nil {
		return err
	}
	if err := repo.dataAccessor.DeleteLLMProvider(providerId); err != nil {
		return err
	}
	idx := common.FindIndex(repo.llmProviders, func(p dataobject.TLLMProvider) bool {
		return p.ProviderId == providerId
	})
	if idx >= 0 {
		_ = repo.llmProviders.Remove(idx)
	}
	repo.lastLoaded = common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp //time.Now()
	return nil
}

func (repo *AgentRepository) GetAgents(comCode string) []dataobject.TAgent {
	repo.refreshIfExpired()

	out := make([]dataobject.TAgent, 0, repo.agents.Len())
	repo.agents.Range(func(_ int, item dataobject.TAgent) bool {
		if strings.EqualFold(comCode, item.OwnerId) {
			out = append(out, item)
		}
		return true
	})

	return out
}

func (repo *AgentRepository) AddAgent(agent dataobject.TAgent) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	if err := repo.requireAccessor(); err != nil {
		return err
	}
	if err := repo.dataAccessor.CreateAgent(&agent); err != nil {
		return err
	}
	repo.agents.Append(agent)
	repo.lastLoaded = common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp //time.Now() // 本地缓存已与 DB 同步,重置 TTL 计时
	return nil
}

func (repo *AgentRepository) UpdateAgent(agent dataobject.TAgent) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	if err := repo.requireAccessor(); err != nil {
		return err
	}
	if err := repo.dataAccessor.UpdateAgent(&agent); err != nil {
		return err
	}
	idx := common.FindIndex(repo.agents, func(a dataobject.TAgent) bool {
		return a.AgentId == agent.AgentId
	})
	if idx >= 0 {
		_ = repo.agents.Update(idx, agent)
	}
	repo.lastLoaded = common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp //time.Now()
	return nil
}

func (repo *AgentRepository) RemoveAgent(agentId string) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	if err := repo.requireAccessor(); err != nil {
		return err
	}
	if err := repo.dataAccessor.DeleteAgent(agentId); err != nil {
		return err
	}
	idx := common.FindIndex(repo.agents, func(a dataobject.TAgent) bool {
		return a.AgentId == agentId
	})
	if idx >= 0 {
		_ = repo.agents.Remove(idx)
	}
	repo.lastLoaded = common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp //time.Now()
	return nil
}

func (repo *AgentRepository) GetSkills(comCode string) []dataobject.TSkill {
	repo.refreshIfExpired()

	out := make([]dataobject.TSkill, 0, repo.skills.Len())
	repo.skills.Range(func(_ int, item dataobject.TSkill) bool {
		if strings.EqualFold(comCode, item.OwnerId) {
			out = append(out, item)
		}
		return true
	})

	return out
}

func (repo *AgentRepository) AddSkill(skill dataobject.TSkill) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	if err := repo.requireAccessor(); err != nil {
		return err
	}
	if err := repo.dataAccessor.CreateSkill(&skill); err != nil {
		return err
	}
	repo.skills.Append(skill)
	repo.lastLoaded = common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp //time.Now() // 本地缓存已与 DB 同步,重置 TTL 计时
	return nil
}

func (repo *AgentRepository) UpdateSkill(skill dataobject.TSkill) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	if err := repo.requireAccessor(); err != nil {
		return err
	}
	if err := repo.dataAccessor.UpdateSkill(&skill); err != nil {
		return err
	}
	idx := common.FindIndex(repo.skills, func(s dataobject.TSkill) bool {
		return s.SkillId == skill.SkillId
	})
	if idx >= 0 {
		_ = repo.skills.Update(idx, skill)
	}
	repo.lastLoaded = common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp //time.Now()
	return nil
}

func (repo *AgentRepository) RemoveSkill(skillId string) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	if err := repo.requireAccessor(); err != nil {
		return err
	}
	if err := repo.dataAccessor.DeleteSkill(skillId); err != nil {
		return err
	}
	idx := common.FindIndex(repo.skills, func(s dataobject.TSkill) bool {
		return s.SkillId == skillId
	})
	if idx >= 0 {
		_ = repo.skills.Remove(idx)
	}
	repo.lastLoaded = common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp //time.Now()
	return nil
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

func (repo *AgentRepository) loadAllAgents() {
	// 重新加载前清空,避免重复追加
	repo.llmProviders.Clear()
	repo.agents.Clear()
	repo.skills.Clear()

	repo.loadLLMProviders()
	repo.loadAgents()
	repo.loadSkills()

	// 记录本次加载时间并复位刷新标记(供 TTL 判断与防重载风暴使用)
	repo.lastLoaded = common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp //time.Now()
	repo.refreshing = false
}

func (repo *AgentRepository) loadLLMProviders() {
	if err := repo.requireAccessor(); err != nil {
		return
	}
	providers, err := repo.dataAccessor.GetAllLLMProviders()
	if err != nil {
		return
	}
	for _, p := range providers {
		repo.llmProviders.Append(p)
	}
}

func (repo *AgentRepository) loadAgents() {
	if err := repo.requireAccessor(); err != nil {
		return
	}
	agents, err := repo.dataAccessor.GetAllAgents()
	if err != nil {
		return
	}
	for _, a := range agents {
		repo.agents.Append(a)
	}
}

func (repo *AgentRepository) loadSkills() {
	if err := repo.requireAccessor(); err != nil {
		return
	}
	skills, err := repo.dataAccessor.GetAllSkills()
	if err != nil {
		return
	}
	for _, s := range skills {
		repo.skills.Append(s)
	}
}

// requireAccessor 检查 data accessor 是否可用(GetAgentRepository 可能因数据库类型不支持返回 nil)
func (repo *AgentRepository) requireAccessor() error {
	if repo.dataAccessor == nil {
		return exception.UnsupportedExceptionErrorEx("agent repo: data accessor is nil")
	}
	return nil
}

// refreshIfExpired 在读取缓存前调用:当缓存超过 TTL 时触发一次全量重载(多实例一致性)。
//   - configTTL<=0 时禁用(仅启动时加载一次);
//   - refreshing 标记防止并发读造成"重载风暴":同一时刻只有一个 goroutine 真正重载,
//     其余并发读继续返回旧缓存,不阻塞业务;
//   - 实际重载由 loadAllAgents 完成,其内部重新加锁并在结束后更新 lastLoaded/refreshing。
func (repo *AgentRepository) refreshIfExpired() {
	if repo.configTTL <= 0 {
		return
	}

	nowTimestamp := common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	repo.mu.Lock()
	expired := (nowTimestamp - repo.lastLoaded) >= repo.configTTL
	if !expired || repo.refreshing {
		repo.mu.Unlock()
		return
	}
	repo.refreshing = true // 先标记再释放锁,阻止其他 goroutine 重复发起重载
	repo.mu.Unlock()

	// 锁外重载,可能会导致 TTL时间过了，存在 几个CPU 时钟周期的缓存
	repo.loadAllAgents()
}
