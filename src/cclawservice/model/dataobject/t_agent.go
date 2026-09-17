/**
* 定义所有的供应商的数据对象，包括LLM等
**/

package dataobject

import (
	"encoding/json"
	"strings"

	"gorm.io/plugin/optimisticlock"
)

// TLlm 表示大模型供应商数据对象
type TLlm struct {
	OwnerId      string `json:"OwnerId"`
	ProviderId   string `json:"providerId"`   //供应商ID
	ProviderName string `json:"providerName"` //供应商名称
	AliasName    string `json:"aliasName"`    //供应商别名

	BaseUrl string `json:"baseUrl"` //供应商的基础URL
	Model   string `json:"model"`   //实际请求使用的模型名(厂商命名,如 deepseek-v4-pro);为空回退 ProviderId
	ApiKey  string `json:"-"`       //api key
	Vendor  string `json:"vendor"`  //LLM厂家名称,如deepseek,anthropic,OpenAI等
	Api     string `json:"api"`     //供应商的API协议类型

	ThkNonStreaming bool `json:"thinkingNonStreaming"` //非流模式下是否支持思考,false/true=否/是

	Reasoning   bool `json:"reasoning"`   //是否支持推理
	ReasoningEx bool `json:"reasoningEx"` //是否支持推理力度参数

	Input int `json:"Input"` //支持的输入模态:0/1/2:text/image/audio...

	InputCost       float32 `json:"InputCost"`       //每百万token输入的单价
	OutputCost      float32 `json:"OutputCost"`      //每百万token输出的单价
	CachedReadCost  float32 `json:"CachedReadCost"`  //每百万命中缓存读的单价
	CachedWriteCost float32 `json:"CachedWriteCost"` //每百万命中缓存写的单价

	ContextWindow uint32 `json:"ContextWindow"`

	MaxTokens        int  `json:"MaxTokens"` // 单词聊天返回的最大的token数量大小
	UsageInStreaming bool `json:"UsageInStreaming"`

	AuthMode uint8 `json:"AuthMode"`

	Disabled bool `json:"Disabled"`
	Deleted  bool `json:"Deleted"`

	DVersion optimisticlock.Version `json:"dVersion"` //乐观锁
}

func LlmValues(p *TLlm) map[string]interface{} {
	return map[string]interface{}{
		"owner_id":           p.OwnerId,
		"provider_name":      p.ProviderName,
		"alias_name":         p.AliasName,
		"base_url":           p.BaseUrl,
		"model":              p.Model,
		"vendor":             p.Vendor,
		"api":                p.Api,
		"thk_non_streaming":  p.ThkNonStreaming,
		"reasoning":          p.Reasoning,
		"reasoning_ex":       p.ReasoningEx,
		"input":              p.Input,
		"input_cost":         p.InputCost,
		"output_cost":        p.OutputCost,
		"cached_read_cost":   p.CachedReadCost,
		"cached_write_cost":  p.CachedWriteCost,
		"context_window":     p.ContextWindow,
		"max_tokens":         p.MaxTokens,
		"usage_in_streaming": p.UsageInStreaming,
		"auth_mode":          p.AuthMode,
		"disabled":           p.Disabled,
	}
}

// GetModelName 返回请求厂商使用的模型名(provider.Model 优先,未配置回退 ProviderId)。
func (llm *TLlm) GetModelName() string {
	if m := strings.TrimSpace(llm.Model); m != "" {
		return m
	}

	return llm.ProviderId
}

func (llm *TLlm) ToJsonString() (string, error) {
	jsonSting, err := json.Marshal(llm)
	if err != nil {
		return "", err
	}

	return string(jsonSting), nil
}

func (llm *TLlm) FromJsonString(jsonStr string) error {
	err := json.Unmarshal([]byte(jsonStr), llm)
	if err != nil {
		return err
	}

	return nil
}

type TAgent struct {
	OwnerId      string `json:"OwnerId"`
	AgentId      string `json:"AgentId"`
	AgentName    string `json:"AgentName"`
	Description  string `json:"Description"`
	PrimaryModel string `json:"PrimaryModel"` //主要使用的AI模型,如deepseek,qwen-3.8-flash等
	Fallbacks    string `json:"Fallbacks"`
	AgentDir     string `json:"AgentDir"`  //agent启动文件所在的路径
	Workspace    string `json:"Workspace"` //表示该agent的工作空间
	IsDefault    bool   `json:"IsDefault"`

	ImageName  string `json:"ImageName"` //agent的公众形象名称
	ImageEmoji string `json:"ImageEmoji"`
	ImageTheme string `json:"ImageTheme"`

	CompactionMode      int `json:"CompactionMode"`      //压缩模式
	BootstrapTotalChars int `json:"BootstrapTotalChars"` //启动文档注入总上限(rune):0/未配置 → MaxBootstrapTotalChars;负数 → 不限制

	ReverseTokenFloor       int `json:"ReverseTokenFloor"` // 为系统提示(7 个启动文档 + 折叠进 system 的摘要)预留的最小 token 额度;只在该值大于实测系统提示估量时才生效
	ReverseTokenFloorTokens int `gorm:"-" json:"ReverseTokenFloorTokens"`

	// MaxHistoryTokens 历史回放上限(tokens):0/未配置 → 用 hCommon.DefaultMaxHistoryTokens;负数 → 不限制。
	// 只作用于"历史回放"这一项预算,不影响工具循环(FitMessages)—— 想多早开始压缩历史就调它。
	MaxHistoryTokens    int `gorm:"column:max_history_tokens" json:"MaxHistoryTokens"`
	MemorySummarySize   int `json:"MemorySummarySize"` //长期记忆摘要占比
	MemorySummaryTokens int `gorm:"-" json:"MemorySummaryTokens"`
	MemorySize          int `json:"MemorySize"` // 历史记忆占比
	MemoryTokens        int `gorm:"-" json:"MemoryTokens"`
	CurrentSize         int `json:"CurrentSize"` //当前用户输入+工具输出结果占比
	CurrentTokens       int `gorm:"-" json:"CurrentTokens"`

	MaxConcurrents int `gorm:"column:max_concurrent" json:"MaxConcurrents"` // DDL 列为 max_concurrent
	SubAgents      int `json:"SubAgents"`
	ReasoningMode  int `json:"ReasoningMode"` //推理展示的方式 0/1/2:off/生成后附带/2 streaming
	ToolProgress   int `json:"ToolProgress"`  //工具执行的进度

	SkillIds []string `gorm:"-" json:"SkillIds"` //与之关联的skill

	Deleted  bool                   `json:"Deleted"`
	Disabled bool                   `json:"Disabled"`
	DVersion optimisticlock.Version `json:"dVersion"`
}

func AgentValues(a *TAgent) map[string]interface{} {
	return map[string]interface{}{
		"owner_id":              a.OwnerId,
		"agent_name":            a.AgentName,
		"description":           a.Description,
		"primary_model":         a.PrimaryModel,
		"fallbacks":             a.Fallbacks,
		"agent_dir":             a.AgentDir,
		"workspace":             a.Workspace,
		"is_default":            a.IsDefault,
		"image_name":            a.ImageName,
		"image_emoji":           a.ImageEmoji,
		"image_theme":           a.ImageTheme,
		"compaction_mode":       a.CompactionMode,
		"bootstrap_total_chars": a.BootstrapTotalChars,
		"reverse_token_floor":   a.ReverseTokenFloor,
		"max_history_tokens":    a.MaxHistoryTokens,
		"memory_summary_size":   a.MemorySummarySize,
		"memory_summary_tokens": a.MemorySummaryTokens,
		"memory_size":           a.MemorySize,
		"memory_tokens":         a.MemoryTokens,
		"current_size":          a.CurrentSize,
		"current_tokens":        a.CurrentTokens,
		"max_concurrent":        a.MaxConcurrents,
		"subagents":             a.SubAgents,
		"reasoning_mode":        a.ReasoningMode,
		"tool_progress":         a.ToolProgress,
		"skill_ids":             a.SkillIds,
		"deleted":               a.Deleted,
		"disabled":              a.Disabled,
	}
}

func (agent *TAgent) ToJsonString() (string, error) {
	jsonSting, err := json.Marshal(agent)
	if err != nil {
		return "", err
	}

	return string(jsonSting), nil
}

func (agent *TAgent) FromJsonString(jsonStr string) error {
	err := json.Unmarshal([]byte(jsonStr), agent)
	if err != nil {
		return err
	}
	return nil
}

// SkillScope 技能作用域(与 t_skill.skill_scope 对齐):决定技能对哪些归属可见。
//
// 与 t_agent_skill 的分工:关联表决定"某 agent 是否启用该技能",作用域决定
// "该技能能否被该归属使用"(见 TSkill.IsSkillVisibleTo),两者同时满足才生效。
type SkillScope int

const (
	SkillScopeSystem   SkillScope = iota // 0 系统:平台级技能,所有归属可见(如 owner_id=000000)
	SkillScopePersonal                   // 1 个人:仅技能归属(owner_id)自身可见
	SkillScopeProject                    // 2 项目:仅技能归属自身可见(当前无项目维度,按归属判定)
	SkillScopePlugin                     // 3 插件:随插件分发的技能,所有归属可见
)

func (s SkillScope) String() string {
	switch s {
	case SkillScopeSystem:
		return "system"
	case SkillScopePersonal:
		return "personal"
	case SkillScopeProject:
		return "project"
	case SkillScopePlugin:
		return "plugin"
	default:
		return "unknown"
	}
}

// Valid 判断作用域取值是否合法(0..3)。
func (s SkillScope) Valid() bool {
	return s >= SkillScopeSystem && s <= SkillScopePlugin
}

type TSkill struct {
	OwnerId    string `json:"OwnerId"`
	SkillId    string `json:"skillId"`
	SkillName  string `json:"skillName"`
	SkillDir   string `json:"skillDir"`   // 技能目录名(相对 skills 根的安全相对路径),其下固定为 SKILL.md
	SkillScope int    `json:"skillScope"` // 作用域,见 SkillScope(0系统/1个人/2项目/3插件)

	Disabled bool                   `json:"Disabled"`
	Deleted  bool                   `json:"Deleted"`
	DVersion optimisticlock.Version `json:"dVersion"`
}

func SkillValues(sk *TSkill) map[string]interface{} {
	return map[string]interface{}{
		"owner_id":    sk.OwnerId,
		"skill_name":  sk.SkillName,
		"skill_dir":   sk.SkillDir,
		"skill_scope": sk.SkillScope,
		"disabled":    sk.Disabled,
	}
}

// Scope 返回强类型作用域(便于与 SkillScope 常量比较)。
func (sk *TSkill) Scope() SkillScope { return SkillScope(sk.SkillScope) }

// IsSkillVisibleTo 判断该技能对指定归属(comCode/owner_id)是否可见。
//   - 系统/插件作用域:全局可见(平台技能);
//   - 个人/项目作用域:仅当技能归属与使用方归属一致时可见(防跨单位越权引用)。
func (sk *TSkill) IsSkillVisibleTo(ownerId string) bool {
	if scope := sk.Scope(); scope == SkillScopeSystem || scope == SkillScopePlugin {
		return true
	}

	return strings.EqualFold(strings.TrimSpace(sk.OwnerId), strings.TrimSpace(ownerId))
}

func (sk *TSkill) ToJsonString() (string, error) {
	jsonSting, err := json.Marshal(sk)
	if err != nil {
		return "", err
	}
	return string(jsonSting), nil
}

func (sk *TSkill) FromJsonString(jsonStr string) error {
	err := json.Unmarshal([]byte(jsonStr), sk)
	if err != nil {
		return err
	}
	return nil
}
