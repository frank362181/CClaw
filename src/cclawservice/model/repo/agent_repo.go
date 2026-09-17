/**
* 从数据库中获取所有的llm providers,agent,skill等信息
* 这里并不会加载agent的启动文件以及SKILL.md等文件内容
**/

package repo

import (
	"math"
	"slices"
	"strings"
	"sync"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/model/dataaccessor"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/database/core"
	"my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
)

type AgentRepository struct {
	mu sync.RWMutex

	dataAccessor dataaccessor.IAgentAccessor

	llmProviders *common.SafeList[dataobject.TLlm]
	baseTools    *common.SafeList[dataobject.TTool]
	agents       *common.SafeList[dataobject.TAgent]
	skills       *common.SafeList[dataobject.TSkill]
	routeRules   *common.SafeList[dataobject.TAgentRouteRule]

	// 配置缓存 TTL 刷新
	configTTL  int64 // 缓存过期时间;<=0 表示禁用 TTL 刷新(仅启动时加载一次)
	lastLoaded int64 // 最近一次从 DB 全量加载的时间(秒级时间戳)

	// 刷新协调(singleflight):reloading=true 表示已有 goroutine 正在全量重载,
	// 其余读直接使用现有快照,避免并发重复全量查询(读放大)。
	reloading bool

	dataVersion int64
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

	GetBaseTools() []dataobject.TTool
	GetBaseTool(toolId string) (dataobject.TTool, bool)
	AddBaseTool(tool dataobject.TTool) error
	UpdateBaseTool(tool dataobject.TTool) error
	RemoveBaseTool(toolId string) error

	GetSkills(comCode string) []dataobject.TSkill
	GetSkillsByAgent(agent dataobject.TAgent) []dataobject.TSkill
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
	agentRepository.dataAccessor, _ = dataaccessor.GetAgentAccessor(db)
	return agentRepository
}

func (repo *AgentRepository) LoadAllAgents(ttl int64) []dataobject.TAgent {
	repo.mu.Lock()
	repo.configTTL = int64(math.Max(0, float64(ttl)))
	repo.mu.Unlock()

	// 按最新配置强制执行一次全量重载(启动/管理界面变更后同步;锁外 DB IO)
	repo.reloadIfNeeded(true)

	repo.mu.RLock()
	agents := repo.agents
	repo.mu.RUnlock()

	out := make([]dataobject.TAgent, 0, agents.Len())
	agents.Range(func(_ int, item dataobject.TAgent) bool {
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
	repo.dataVersion++                                                                   // 写已提交:使进行中的全量重载作废并重试,避免旧快照覆盖本次变更
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
	repo.dataVersion++                                                                   // 写已提交:使进行中的全量重载作废并重试
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
	repo.dataVersion++                                                                   // 写已提交:使进行中的全量重载作废并重试
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
	repo.dataVersion++                                                                   // 写已提交:使进行中的全量重载作废并重试
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
	repo.dataVersion++                                                                   // 写已提交:使进行中的全量重载作废并重试
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
	repo.dataVersion++                                                                   // 写已提交:使进行中的全量重载作废并重试
	return nil
}

func (repo *AgentRepository) GetBaseTools() []dataobject.TTool {
	repo.refreshIfExpired()

	out := make([]dataobject.TTool, 0, repo.baseTools.Len())
	repo.baseTools.Range(func(_ int, item dataobject.TTool) bool {
		out = append(out, item)
		return true
	})

	return out
}

func (repo *AgentRepository) GetBaseTool(toolId string) (dataobject.TTool, bool) {
	repo.refreshIfExpired()

	var found dataobject.TTool
	ok := false
	repo.baseTools.Range(func(_ int, item dataobject.TTool) bool {
		if strings.EqualFold(toolId, item.ToolId) {
			found = item
			ok = true
			return false
		}
		return true
	})

	return found, ok
}

func (repo *AgentRepository) AddBaseTool(tool dataobject.TTool) error {
	if err := repo.requireAccessor(); err != nil {
		return err
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if err := repo.dataAccessor.CreateBaseTool(&tool); err != nil {
		return err
	}

	repo.baseTools.Append(tool)
	repo.lastLoaded = common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	repo.dataVersion++
	return nil
}

func (repo *AgentRepository) UpdateBaseTool(tool dataobject.TTool) error {
	if err := repo.requireAccessor(); err != nil {
		return err
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if err := repo.dataAccessor.UpdateBaseTool(&tool); err != nil {
		return err
	}
	idx := common.FindIndex(repo.baseTools, func(t dataobject.TTool) bool {
		return t.ToolId == tool.ToolId
	})

	if idx >= 0 {
		_ = repo.baseTools.Update(idx, tool)
	}
	repo.lastLoaded = common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	repo.dataVersion++
	return nil
}

func (repo *AgentRepository) RemoveBaseTool(toolId string) error {
	if err := repo.requireAccessor(); err != nil {
		return err
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()

	if err := repo.dataAccessor.DeleteBaseTool(toolId); err != nil {
		return err
	}

	idx := common.FindIndex(repo.baseTools, func(t dataobject.TTool) bool {
		return t.ToolId == toolId
	})
	if idx >= 0 {
		_ = repo.baseTools.Remove(idx)
	}
	repo.lastLoaded = common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	repo.dataVersion++
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

func (repo *AgentRepository) GetSkillsByAgent(agent dataobject.TAgent) []dataobject.TSkill {
	if len(agent.SkillIds) == 0 {
		return nil
	}

	repo.refreshIfExpired()
	out := make([]dataobject.TSkill, 0, len(agent.SkillIds))

	repo.skills.Range(func(_ int, item dataobject.TSkill) bool {
		if slices.Contains(agent.SkillIds, item.SkillId) {
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
	repo.dataVersion++                                                                   // 写已提交:使进行中的全量重载作废并重试
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
	repo.dataVersion++                                                                   // 写已提交:使进行中的全量重载作废并重试
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
	repo.dataVersion++                                                                   // 写已提交:使进行中的全量重载作废并重试
	return nil
}

func (repo *AgentRepository) Destroy() {
	if repo.llmProviders != nil {
		repo.llmProviders.Clear()
	}

	if repo.agents != nil {
		repo.agents.Clear()
	}

	if repo.baseTools != nil {
		repo.baseTools.Clear()
	}

	if repo.skills != nil {
		repo.skills.Clear()
	}

	if repo.routeRules != nil {
		repo.routeRules.Clear()
	}
}

// ============================== private functions & methods =========================

func (repo *AgentRepository) currentTimestamp() int64 {
	return common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
}

// isExpiredLocked 判断配置缓存是否已超过 TTL(调用方须持有 mu 的读或写锁)。
func (repo *AgentRepository) isExpiredLocked(now int64) bool {
	return repo.configTTL > 0 && now-repo.lastLoaded >= repo.configTTL
}

// newSafeListFrom 由切片构造线程安全列表(copy-on-write 换新快照用)。
func newSafeListFrom[T any](items []T) *common.SafeList[T] {
	list := common.NewSafeList[T]()
	for _, item := range items {
		list.Append(item)
	}
	return list
}

// loadAllFromDB 在锁外执行四张配置表的全量读取,构造全新快照(不触碰现有缓存)。
// 任一查询失败返回 err 且不产生可替换的快照;调用方应保留旧缓存,避免"先清后载"清空可用配置。
func (repo *AgentRepository) loadAllFromDB() (*common.SafeList[dataobject.TLlm], *common.SafeList[dataobject.TAgent], *common.SafeList[dataobject.TTool], *common.SafeList[dataobject.TSkill], *common.SafeList[dataobject.TAgentRouteRule], error) {
	if err := repo.requireAccessor(); err != nil {
		return nil, nil, nil, nil, nil, err
	}

	providers, err := repo.dataAccessor.GetAllLLMProviders()
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	agents, err := repo.dataAccessor.GetAllAgents()
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	tools, err := repo.dataAccessor.GetBaseTools()
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	skills, err := repo.dataAccessor.GetAllSkills()
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	rules, err := repo.dataAccessor.GetAllAgentRouteRules()
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	return newSafeListFrom(providers), newSafeListFrom(agents), newSafeListFrom(tools), newSafeListFrom(skills), newSafeListFrom(rules), nil
}

// reloadIfNeeded 在 TTL 到期(或 force)时,以 singleflight 方式执行一次全量重载:
//   - 未过期时仅一次 RLock 快判,不持写锁、不发 IO;
//   - 过期后由单个 goroutine 认领(锁外 DB 读取),其余调用者见 reloading=true 直接使用旧快照;
//   - 成功后以"整体换引用(copy-on-write)"更新缓存(短临界区),避免持锁做 IO;
//   - 加载失败保留旧缓存并告警;若加载期间有写提交(dataVersion 变化)则放弃本次快照并重试,
//     防止用较旧 DB 快照覆盖刚提交的变更(有界重试,最多 maxReloadAttempts 轮)。
func (repo *AgentRepository) reloadIfNeeded(force bool) {
	if !force && repo.configTTL <= 0 {
		return
	}

	const maxReloadAttempts = 3
	for attempt := 0; attempt < maxReloadAttempts; attempt++ {
		repo.mu.RLock()
		reloading := repo.reloading
		expired := force || repo.isExpiredLocked(repo.currentTimestamp())
		versionStart := repo.dataVersion
		repo.mu.RUnlock()

		if reloading {
			return // 已有人在刷新:直接使用当前快照(singleflight)
		}
		if !expired {
			return
		}

		// 竞争刷新权(double-check,短写锁)
		repo.mu.Lock()
		if repo.reloading || (!force && !repo.isExpiredLocked(repo.currentTimestamp())) {
			repo.mu.Unlock()
			return
		}
		repo.reloading = true
		repo.mu.Unlock()

		// 锁外 DB 全量读取(真正耗时的部分,不阻塞任何读写)
		providers, agents, tools, skills, rules, loadErr := repo.loadAllFromDB()

		repo.mu.Lock()
		repo.reloading = false
		if loadErr != nil {
			repo.mu.Unlock()
			logging.LogWarnF("agent repo: reload config cache failed, keep old cache, err:%s", loadErr.Error())
			return
		}
		if repo.dataVersion != versionStart {
			// 重载期间有写提交:本次快照可能不是最新,放弃替换,下轮重试
			repo.mu.Unlock()
			continue
		}

		// copy-on-write:整体换引用(短临界区)
		repo.llmProviders = providers
		repo.agents = agents
		repo.baseTools = tools
		repo.skills = skills
		repo.routeRules = rules
		repo.lastLoaded = repo.currentTimestamp()
		repo.mu.Unlock()
		return
	}
	logging.LogWarnF("agent repo: reload config cache conflicted with concurrent writes, will retry on next TTL")
}

// requireAccessor 检查 data accessor 是否可用(GetAgentRepository 可能因数据库类型不支持返回 nil)
func (repo *AgentRepository) requireAccessor() error {
	if repo.dataAccessor == nil {
		return exception.UnsupportedExceptionErrorEx("agent repo: data accessor is nil")
	}
	return nil
}

// refreshIfExpired 在读取缓存前调用:仅当缓存超过 TTL 时触发一次全量重载(singleflight)。
//   - 未过期时只做一次 RLock 快判,不持写锁、不发 IO;
//   - 过期时由单个 goroutine 在锁外重载,其余读直接使用旧快照;
//   - 多实例一致性:各实例独立按 TTL 拉取最新配置,重载不阻塞当前请求。
func (repo *AgentRepository) refreshIfExpired() {
	repo.reloadIfNeeded(false)
}
