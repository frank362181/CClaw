/**
* Agent 启动文档的数据对象:AGENTS.md(agent.md)/SOUL.md(soul.md)/TOOLS.md(tools.md)
*
* 每个文档由两部分组成:
*   1) FrontMatter: 文档顶部的 YAML 元数据(强类型,避免魔法 map)
*   2) Content:     Markdown 正文
**/

package dataobject

import (
	"encoding/json"

	hCommon "my.favorite/cclawservice/harness/common"
)

// AgentFrontMatter 是 agent 启动文档的 YAML 元数据
type AgentFrontMatter struct {
	Summary  string   `json:"summary,omitempty" yaml:"summary,omitempty"`
	Title    string   `json:"title,omitempty" yaml:"title,omitempty"`
	ReadWhen []string `json:"readWhen,omitempty" yaml:"read_when,omitempty"`
}

// AgentDocument 表示一个已解析的 agent 启动文档
type AgentDocument struct {
	Kind        hCommon.AgentDocumentKind `json:"kind"`
	FileName    string                    `json:"fileName"`
	FrontMatter AgentFrontMatter          `json:"frontMatter"`
	Content     string                    `json:"content"`
	ByteSize    int64                     `json:"byteSize"` // 原始文件字节数,用于缓存指纹与日志

	Missing  bool `json:"missing"`  // 必需文件缺失(OpenClaw WorkspaceBootstrapFile.missing)
	Required bool `json:"required"` // 是否为必需文件;缺失时注入 [MISSING] 标记,可选文件缺失则省略
}

// ToJsonString 序列化为 JSON 字符串
func (doc *AgentDocument) ToJsonString() (string, error) {
	jsonStr, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}

	return string(jsonStr), nil
}

// FromJsonString 从 JSON 字符串反序列化
func (doc *AgentDocument) FromJsonString(jsonStr string) error {
	return json.Unmarshal([]byte(jsonStr), doc)
}

// ClassifyAgent 是路由分类 prompt 中的单个 Agent 候选。
// Label 为用于意图判断的"简介",由调用方按优先级解析:
// AGENTS.md 的 front matter summary > 数据库 description;两者皆空回退到Agent名称。
type ClassifyAgent struct {
	AgentId   string
	AgentName string
	Label     string
}

/**
* SKILL.md 技能文档的数据对象
*
* SKILL.md 的 YAML Front Matter(元数据)字段与 agentos/agent_skills 的 SkillMetadata 保持一致:
* name / description / user-invocable / disable-mode-invocation / emoji / home_page / os / requires
**/

// SkillRequires 是 SKILL 运行所需的依赖环境
type SkillRequires struct {
	Bins    []string `json:"bins,omitempty" yaml:"bins,omitempty"`
	AnyBins []string `json:"anyBins,omitempty" yaml:"any_bins,omitempty"`
	Env     []string `json:"env,omitempty" yaml:"env,omitempty"`
	Config  []string `json:"config,omitempty" yaml:"config,omitempty"`
}

// SkillTool 是 SKILL.md front matter 中声明的一个可调用工具(供 function-calling 注册)。
//   - Name/Description/Parameters:用于生成给 LLM 的函数 schema;
//   - Script:相对 SKILL.md 所在目录的脚本路径(如 scripts/weather.py),执行时以参数 JSON 写入 stdin,
//     脚本把结果(JSON/文本)写到 stdout,退出码非 0 表示失败(stderr 为错误信息)。
type SkillTool struct {
	Name        string         `json:"name,omitempty" yaml:"name,omitempty"`
	Description string         `json:"description,omitempty" yaml:"description,omitempty"`
	Script      string         `json:"script,omitempty" yaml:"script,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty" yaml:"parameters,omitempty"`
}

// SkillFrontMatter 是 SKILL.md 的 YAML 元数据部分
type SkillFrontMatter struct {
	Name                  string        `json:"name,omitempty" yaml:"name,omitempty"`
	Description           string        `json:"description,omitempty" yaml:"description,omitempty"`
	UserInvocable         bool          `json:"userInvocable,omitempty" yaml:"user-invocable,omitempty"`
	DisableModeInvocation bool          `json:"disableModeInvocation,omitempty" yaml:"disable-mode-invocation,omitempty"`
	Emoji                 string        `json:"emoji,omitempty" yaml:"emoji,omitempty"`
	HomePage              string        `json:"homePage,omitempty" yaml:"home_page,omitempty"`
	OS                    []string      `json:"os,omitempty" yaml:"os,omitempty"`
	Version               string        `json:"version,omitempty" yaml:"version,omitempty"`
	Requires              SkillRequires `json:"requires,omitempty" yaml:"requires,omitempty"`
	Tools                 []SkillTool   `json:"tools,omitempty" yaml:"tools,omitempty"`
}

// SkillDocument 表示一个已解析的 SKILL.md 文档
type SkillDocument struct {
	SkillId     string           `json:"skillId"`
	SkillFile   string           `json:"skillFile"` // SKILL.md 的完整路径
	FrontMatter SkillFrontMatter `json:"frontMatter"`
	Content     string           `json:"content"`
	ByteSize    int64            `json:"byteSize"`
}

// ToJsonString 序列化为 JSON 字符串
func (doc *SkillDocument) ToJsonString() (string, error) {
	jsonStr, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}

	return string(jsonStr), nil
}

// FromJsonString 从 JSON 字符串反序列化
func (doc *SkillDocument) FromJsonString(jsonStr string) error {
	return json.Unmarshal([]byte(jsonStr), doc)
}
