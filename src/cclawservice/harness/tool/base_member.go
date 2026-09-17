/**
* description:
*	定义工具的基础类、常量等
**/

package tool

type TSource int // tool的来源
const (
	TSourceSystem TSource = iota // 基础工具
	TSourceSkill                 //来自于plugin的工具
	TSourcePlugin
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
		return "system"
	case TSourceSkill:
		return "skill"
	case TSourcePlugin:
		return "plugin"
	default:
		return "unknown"
	}
}

type BaseTool struct {
	ToolId      string `json:"toolId"`
	ToolName    string `json:"toolName"`
	Description string `json:"description"`
	Script      string `json:"script"` //py脚本的路径
	Schema      string `json:"schema"`

	ToolSrc TSource `json:"toolSrc"`
}

type TBox struct {
	ToolName string // tool name

}
