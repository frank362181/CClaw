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

	llmProviders *common.SafeList[dataobject.TLlm]
	agents       *common.SafeList[dataobject.TAgent]
	skills       *common.SafeList[dataobject.TSkill]
	routeRules   *common.SafeList[dataobject.TAgentRouteRule]

	// 配置缓存 TTL 刷新
	configTTL  int64 // 缓存过期时间;<=0 表示禁用 TTL 刷新(仅启动时加载一次)
	lastLoaded int64 // 最近一次从 DB 全量加载的时间(秒级时间戳)
}

type IAgentRepository interface {
	LoadAllAgents(ttl int64) []dataobject.TAgent

	GetLLMProviders(comCode string) []dataobject.TLlm
	AddLLMProvider(provider dataobject.TLlm) error
	UpdateLLMProvider(provider dataobject.TLlm) error
	RemoveLLMProvider(owerId, providerId string) error

	GetAgents(comCode string) []dataobject.TAgent
	GetAgent(ownerId, agentId string) (dataobject.TAgent, bool)
	AddAgent(agent dataobject.TAgent) error
	UpdateAgent(agent dataobject.TAgent) error
	RemoveAgent(ownerId, agentId string) error

	GetSkills(comCode string) []dataobject.TSkill
	AddSkill(skill dataobject.TSkill) error
	UpdateSkill(skill dataobject.TSkill) error
	RemoveSkill(ownerId, skillId string) error

	GetAgentRouteRules(comCode string) []dataobject.TAgentRouteRule

	Destroy()
}

var agentRepository = common.GetSingleton(func() *AgentRepository {
	return &AgentRepository{
		dataAccessor: nil,
		configTTL:    cCommon.DefaultReloadDatabaseCacheTTLSeconds,

		llmProviders: common.NewSafeList[dataobject.TLlm](),
		agents:       common.NewSafeList[dataobject.TAgent](),
		skills:       common.NewSafeList[dataobject.TSkill](),
		routeRules:   common.NewSafeList[dataobject.TAgentRouteRule](),
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

func (repo *AgentRepository) GetLLMProviders(comCode string) []dataobject.TLlm {
	repo.refreshIfExpired()

	out := make([]dataobject.TLlm, 0, repo.llmProviders.Len())
	repo.llmProviders.Range(func(_ int, item dataobject.TLlm) bool {
		if strings.EqualFold(comCode, item.OwnerId) {
			out = append(out, item)
		}
		return true
	})

	return out
}

func (repo *AgentRepository) AddLLMProvider(provider dataobject.TLlm) error {
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

func (repo *AgentRepository) UpdateLLMProvider(provider dataobject.TLlm) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	if err := repo.requireAccessor(); err != nil {
		return err
	}
	if err := repo.dataAccessor.UpdateLLMProvider(&provider); err != nil {
		return err
	}
	idx := common.FindIndex(repo.llmProviders, func(p dataobject.TLlm) bool {
		return p.ProviderId == provider.ProviderId
	})
	if idx >= 0 {
		_ = repo.llmProviders.Update(idx, provider)
	}
	repo.lastLoaded = common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp //time.Now()
	return nil
}

func (repo *AgentRepository) RemoveLLMProvider(ownerId, providerId string) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	if err := repo.requireAccessor(); err != nil {
		return err
	}
	if err := repo.dataAccessor.DeleteLLMProvider(ownerId, providerId); err != nil {
		return err
	}
	idx := common.FindIndex(repo.llmProviders, func(p dataobject.TLlm) bool {
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

// GetAgent 按业务主键 + 归属查询单个 agent,未找到或不属于该 owner 时返回 ok=false。
// ownerId 必须精确匹配,确保只能访问本单位(comCode)名下的 agent。
func (repo *AgentRepository) GetAgent(ownerId, agentId string) (dataobject.TAgent, bool) {
	repo.refreshIfExpired()

	var found dataobject.TAgent
	ok := false
	repo.agents.Range(func(_ int, item dataobject.TAgent) bool {
		if strings.EqualFold(ownerId, item.OwnerId) && item.AgentId == agentId {
			found = item
			ok = true
			return false // 命中即停止遍历
		}
		return true
	})

	return found, ok
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

func (repo *AgentRepository) RemoveAgent(ownerId, agentId string) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	if err := repo.requireAccessor(); err != nil {
		return err
	}
	if err := repo.dataAccessor.DeleteAgent(ownerId, agentId); err != nil {
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

// GetAgentRouteRules 返回该 comCode 下的内容路由关键词规则(仅未删除、未禁用的在 matchKeyword 中生效)。
func (repo *AgentRepository) GetAgentRouteRules(comCode string) []dataobject.TAgentRouteRule {
	repo.refreshIfExpired()

	out := make([]dataobject.TAgentRouteRule, 0, repo.routeRules.Len())
	repo.routeRules.Range(func(_ int, item dataobject.TAgentRouteRule) bool {
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

func (repo *AgentRepository) RemoveSkill(ownerId, skillId string) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	if err := repo.requireAccessor(); err != nil {
		return err
	}
	if err := repo.dataAccessor.DeleteSkill(ownerId, skillId); err != nil {
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

	if repo.routeRules != nil {
		repo.routeRules.Clear()
	}
}

// ============================== private functions & methods =========================

func (repo *AgentRepository) loadAllAgents() {
	// 重新加载前清空,避免重复追加
	repo.llmProviders.Clear()
	repo.agents.Clear()
	repo.skills.Clear()
	repo.routeRules.Clear()

	if err := repo.requireAccessor(); err != nil {
		return
	}

	// load runtime
	providers, err := repo.dataAccessor.GetAllLLMProviders()
	if err != nil {
		return
	}
	for _, p := range providers {
		repo.llmProviders.Append(p)
	}

	// load agents
	agents, err := repo.dataAccessor.GetAllAgents()
	if err != nil {
		return
	}
	for _, a := range agents {
		repo.agents.Append(a)
	}

	// load skills
	skills, err := repo.dataAccessor.GetAllSkills()
	if err != nil {
		return
	}
	for _, s := range skills {
		repo.skills.Append(s)
	}

	// load route rules
	rules, err := repo.dataAccessor.GetAllAgentRouteRules()
	if err != nil {
		return
	}
	for _, rule := range rules {
		repo.routeRules.Append(rule)
	}

	// 记录本次加载时间(供 TTL 判断使用;调用方需持有 mu)
	repo.lastLoaded = common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
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
//   - 重载在 mu 锁内完成:同一时刻只有一个 goroutine 执行重载,其余并发读/写阻塞在 mu 上,
//     本地缓存因此始终是某个时刻的一致快照(无短暂过期窗口);
//   - 重载结束后由 loadAllAgents 更新 lastLoaded,供下一次 TTL 判断。
func (repo *AgentRepository) refreshIfExpired() {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	if repo.configTTL <= 0 {
		return
	}

	nowTimestamp := common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	if (nowTimestamp - repo.lastLoaded) >= repo.configTTL {
		repo.loadAllAgents()
	}
}
