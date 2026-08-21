/**
* 定义所有的供应商的数据对象，包括LLM等
**/

package dataobject

import (
	"encoding/json"

	"gorm.io/plugin/optimisticlock"
)

// TLLMProvider 表示大模型供应商数据对象
type TLLMProvider struct {
	OwnerId      string `json:"OwnerId"`
	ProviderId   string `json:"providerId"`   //供应商ID
	ProviderName string `json:"providerName"` //供应商名称
	AliasName    string `json:"aliasName"`    //供应商别名

	BaseUrl string `json:"baseUrl"` //供应商的基础URL
	Vendor  string `json:"vendor"`  //LLM厂家名称,如deepseek,anthropic,OpenAI等
	Api     string `json:"api"`     //供应商的API协议类型

	Reasoning   bool `json:"reasoning"`   //是否支持推理
	ReasoningEx bool `json:"reasoningEx"` //是否支持推理力度参数

	Input int `json:"Input"` //支持的输入模态:0/1/2:text/image/audio...

	InputCost       float32 `json:"InputCost"`       //每百万token输入的单价
	OutputCost      float32 `json:"OutputCost"`      //每百万token输出的单价
	CachedReadCost  float32 `json:"CachedReadCost"`  //每百万命中缓存读的单价
	CachedWriteCost float32 `json:"CachedWriteCost"` //每百万命中缓存写的单价

	ContextWindow    uint32 `json:"ContextWindow"`
	MaxTokens        uint32 `json:"MaxTokens"`
	UsageInStreaming bool   `json:"UsageInStreaming"`

	AuthMode uint8 `json:"AuthMode"`

	Disabled bool `json:"Disabled"`
	Deleted  bool `json:"Deleted"`

	DVersion optimisticlock.Version `json:"dVersion"` //乐观锁
}

func (llmProvider *TLLMProvider) ToJsonString() (string, error) {
	jsonSting, err := json.Marshal(llmProvider)
	if err != nil {
		return "", err
	}

	return string(jsonSting), nil
}

func (llmProvider *TLLMProvider) FromJsonString(jsonStr string) error {
	err := json.Unmarshal([]byte(jsonStr), llmProvider)
	if err != nil {
		return err
	}

	return nil
}

type TAgent struct {
	OwnerId      string `json:"OwnerId"`
	AgentId      string `json:"AgentId"`
	AgentName    string `json:"AgentName"`
	PrimaryModel string `json:"PrimaryModel"` //主要使用的AI模型,如deepseek,qwen-3.8-flash等
	Fallbacks    string `json:"Fallbacks"`
	AgentDir     string `json:"AgentDir"`  //agent启动文件所在的路径
	Workspace    string `json:"Workspace"` //表示该agent的工作空间

	ImageName  string `json:"ImageName"` //agent的公众形象名称
	ImageEmoji string `json:"ImageEmoji"`
	ImageTheme string `json:"ImageTheme"`

	CompactionMode    int `json:"CompactionMode"`    //压缩模式
	ReverseTokenFloor int `json:"ReverseTokenFloor"` //压缩时保留的最小token数量

	MaxConcurrents int `json:"MaxConcurrents"`
	SubAgents      int `json:"SubAgents"`
	ReasoningMode  int `json:"ReasoningMode"` //推理展示的方式 0/1/2:off/生成后附带/2 streaming
	ToolProgress   int `json:"ToolProgress"`  //工具执行的进度

	Deleted  bool                   `json:"Deleted"`
	Disabled bool                   `json:"Disabled"`
	DVersion optimisticlock.Version `json:"dVersion"`
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

// TAgentSkills 定义Agent对应的Skill列表
type TAgentSkills struct {
	AgentId string   `json:"agentId"`
	Skills  []TSkill `json:"skills"`
}
