package common

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

// BasicTool 是 SKILL.md front matter/系统工具的json schema 中声明的一个可调用工具(供 function-calling 注册)。
//   - Name/Description/Parameters:用于生成给 LLM 的函数 schema;
//   - Script:脚本路径。front matter 里声明的是相对 SKILL.md 所在目录的路径(如 scripts/weather.py);
//     派生层(tool repository)会解析成**绝对路径**后才下发,执行时以参数 JSON 写入 stdin,
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

type SystemToolSchemaMeta struct {
	Name        string   `json:"name,omitempty" yaml:"name,omitempty"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	InputSchema string   `json:"inputSchema,omitempty" yaml:"inputSchema,omitempty"`
	Required    []string `json:"required,omitempty" yaml:"required,omitempty"`
}

// SystemToolSchema 系统工具的schema
type SystemToolSchema struct {
	ToolName          string `json:"toolName,omitempty" yaml:"toolName,omitempty"`
	SchemaFileName    string `json:"schemaFileName,omitempty" yaml:"schemaFileName,omitempty"`
	SchemaFingerprint string `json:"schemaFingerprint,omitempty" yaml:"schemaFingerprint,omitempty"`
	ByteSize          int64  `json:"byteSize,omitempty" yaml:"byteSize,omitempty"`

	SysToolSchemaMeta SystemToolSchemaMeta `json:"systemToolSchemaMeta,omitempty" yaml:"systemToolSchemaMeta,omitempty"`
}

type SystemicTool struct {
	BasicTool

	Tool              BasicTool
	SchemaFileName    string //对应的schema input所对应的文件
	SchemaFingerprint string //对应的schema文件的fingerprint
}

func NewSystemicTool() *SystemicTool {
	return &SystemicTool{
		BasicTool: BasicTool{
			Source: TSourceSystem,
		},
	}
}

func (st *SystemicTool) GetSchemaFileName() string {
	return st.SchemaFileName
}

func (st *SystemicTool) GetSchemaFingerprint() string {
	return st.SchemaFingerprint
}
