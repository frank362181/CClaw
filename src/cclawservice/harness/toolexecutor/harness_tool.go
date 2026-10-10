package toolexecutor

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
