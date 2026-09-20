/**
* description:
*	定义工具的基础类、常量等
**/

package common

import "encoding/json"

type TSource int // tool的来源
const (
	TSourceSystem TSource = iota // 基础工具
	TSourceSkill                 //来自于plugin的工具
	TSourcePlugin
)

const (
	TSourceUnknown    string = "unknown"
	TSourceSystemName        = "system"
	TSourceSkillName         = "skill"
	TSourcePluginName        = "plugin"
)

// SystemToolName 系统工具的名称,共 6个
type SystemToolName string

const (
	SystemToolGlob  SystemToolName = "Glob"  //搜索路径
	SystemToolGrep  SystemToolName = "Grep"  //搜索文件内容
	SystemToolRead  SystemToolName = "Read"  //读取
	SystemToolWrite SystemToolName = "Write" //覆盖或创建
	SystemToolEdit  SystemToolName = "Edit"  //replace
	SystemToolBash  SystemToolName = "Bash"  //执行
)

func (src TSource) String() string {
	switch src {
	case TSourceSystem:
		return TSourceSystemName
	case TSourceSkill:
		return TSourceSkillName
	case TSourcePlugin:
		return TSourcePluginName
	default:
		return TSourceUnknown
	}
}

type IBasicTool interface {
	ToolSource() TSource
	IsSystemicTool() bool
	IsSkillTool() bool

	GetSchemaFileName() string
	GetSchemaFingerprint() string
}

// BasicTool 是 SKILL.md front matter/系统工具的json schema 中声明的一个可调用工具(供 function-calling 注册)。
//   - Name/Description/Parameters:用于生成给 LLM 的函数 schema;
//   - Script:相对 SKILL.md 所在目录的脚本路径(如 scripts/weather.py),执行时以参数 JSON 写入 stdin,
//     脚本把结果(JSON/文本)写到 stdout,退出码非 0 表示失败(stderr 为错误信息)。
//   - ForceCall:"必须调用"标记(如取实时数据的工具):工具循环首轮会强制 tool_choice=required。
type BasicTool struct {
	Source TSource

	Name        string         `json:"name,omitempty" yaml:"name,omitempty"`
	Description string         `json:"description,omitempty" yaml:"description,omitempty"`
	Script      string         `json:"script,omitempty" yaml:"script,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	ForceCall   bool           `json:"forceCall,omitempty" yaml:"force-call,omitempty"`
}

func (b *BasicTool) ToolSource() TSource {
	return b.Source
}

func (b *BasicTool) IsSystemicTool() bool {
	return b.Source == TSourceSystem
}

func (b *BasicTool) IsSkillTool() bool {
	return b.Source == TSourceSkill
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
	AllowedTools          []string      `json:"allowedTools,omitempty" yaml:"allowedTools,omitempty"`
	DisallowedTools       []string      `json:"disallowedTools,omitempty" yaml:"disallowedTools,omitempty"`
	Tools                 []BasicTool   `json:"tools,omitempty" yaml:"tools,omitempty"`
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
