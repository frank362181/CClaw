/**
* 定义所有的供应商的数据对象，包括LLM等
**/

package dataobject

import (
	"encoding/json"
	"strconv"
	"strings"

	"gorm.io/plugin/optimisticlock"
)

const (
	// CompactionModeDefault 默认模式:把需要压缩的区间整体摘要为一条摘要记录。
	CompactionModeDefault = 0
	// CompactionModeSafeguard 保全模式:分块摘要 + 摘要后校验关键信息(路径/结论/待办)是否保留,
	// 未保留则收窄覆盖区间,宁可多留原文也不丢要点。
	CompactionModeSafeguard = 1
)

// CompactionModeName 返回压缩模式的稳定名称(供日志/可观测性使用,不得用于业务判断)。
func CompactionModeName(mode int) string {
	switch mode {
	case CompactionModeDefault:
		return "default"
	case CompactionModeSafeguard:
		return "safeguard"
	default:
		return "unknown(" + strconv.Itoa(mode) + ")"
	}
}

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
	BootstrapTotalChars int `json:"BootstrapTotalChars"` //启动文件最大的大小
	// ReverseTokenFloor 为系统提示(AGENTS.md 等启动文档 + 折叠进 system 的历史摘要)
	// 预留的最小 token 额度:约束"历史可以占多少",保证规则文档不被历史挤掉。
	// 注意:它**不是**"历史至少要保留多少";取值应明显小于模型 ContextWindow。
	ReverseTokenFloor int `json:"ReverseTokenFloor"`

	MaxConcurrents int `gorm:"column:max_concurrent" json:"MaxConcurrents"` // DDL 列为 max_concurrent
	SubAgents      int `json:"SubAgents"`
	ReasoningMode  int `json:"ReasoningMode"` //推理展示的方式 0/1/2:off/生成后附带/2 streaming
	ToolProgress   int `json:"ToolProgress"`  //工具执行的进度

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
		"max_concurrent":        a.MaxConcurrents,
		"subagents":             a.SubAgents,
		"reasoning_mode":        a.ReasoningMode,
		"tool_progress":         a.ToolProgress,
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

type TSkill struct {
	OwnerId    string `json:"OwnerId"`
	SkillId    string `json:"skillId"`
	SkillName  string `json:"skillName"`
	SkillDir   string `json:"skillDir"`   // 指向skill对应的相对路径,定位SKILL.md
	SkillScope int    `json:"skillScope"` // 指向skill的作用域

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

func (skill *TSkill) ToJsonString() (string, error) {
	jsonSting, err := json.Marshal(skill)
	if err != nil {
		return "", err
	}
	return string(jsonSting), nil
}

func (skill *TSkill) FromJsonString(jsonStr string) error {
	err := json.Unmarshal([]byte(jsonStr), skill)
	if err != nil {
		return err
	}
	return nil
}
