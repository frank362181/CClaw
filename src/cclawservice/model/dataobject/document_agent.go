/**
* Agent 启动文档的数据对象:AGENTS.md(agent.md)/SOUL.md(soul.md)/TOOLS.md(tools.md)
*
* 每个文档由两部分组成:
*   1) FrontMatter: 文档顶部的 YAML 元数据(强类型,避免魔法 map)
*   2) Content:     Markdown 正文
**/

package dataobject

import "encoding/json"

// AgentDocumentKind 定义 agent 启动文档的种类
type AgentDocumentKind int

const (
	AgentDocUnknown   AgentDocumentKind = iota
	AgentDocAgent                       // AGENTS.md
	AgentDocSoul                        // SOUL.md
	AgentDocTools                       // TOOLS.md
	AgentDocMemory                      //MEMORY.md
	AgentDocIdentity                    //IDENTITY.md
	AgentDocUser                        //USER.md
	AgentDocBootstrap                   //BOOTSTRAP.md
)

// String 返回文档种类的可读名称
func (kind AgentDocumentKind) String() string {
	switch kind {
	case AgentDocAgent:
		return "agent"
	case AgentDocSoul:
		return "soul"
	case AgentDocTools:
		return "tools"
	case AgentDocMemory:
		return "memory"
	case AgentDocIdentity:
		return "identity"
	case AgentDocUser:
		return "user"
	case AgentDocBootstrap:
		return "bootstrap"
	default:
		return "unknown"
	}
}

// BootstrapScope 定义启动文档注入的会话范围。
// 参考 OpenClaw filterBootstrapFilesForSession:不同会话形态注入不同的启动文件,
// 并据此控制 MEMORY.md 等隐私敏感文件的可见性。
type BootstrapScope int

const (
	BootstrapScopeNormal    BootstrapScope = iota // 正常会话:注入全部(含 MEMORY.md)
	BootstrapScopeSubagent                        // 子代理会话:仅 AGENTS.md,不含人格/用户/记忆
	BootstrapScopeCron                            // 定时任务:AGENTS/SOUL/IDENTITY/USER,不含 MEMORY.md
	BootstrapScopeHeartbeat                       // 心跳/轻量会话:不注入启动文件
)

// String 返回会话范围的可读名称
func (scope BootstrapScope) String() string {
	switch scope {
	case BootstrapScopeNormal:
		return "normal"
	case BootstrapScopeSubagent:
		return "subagent"
	case BootstrapScopeCron:
		return "cron"
	case BootstrapScopeHeartbeat:
		return "heartbeat"
	default:
		return "unknown"
	}
}

// AgentFrontMatter 是 agent 启动文档的 YAML 元数据
type AgentFrontMatter struct {
	Summary  string   `json:"summary,omitempty" yaml:"summary,omitempty"`
	Title    string   `json:"title,omitempty" yaml:"title,omitempty"`
	ReadWhen []string `json:"readWhen,omitempty" yaml:"read_when,omitempty"`
}

// AgentDocument 表示一个已解析的 agent 启动文档
type AgentDocument struct {
	Kind        AgentDocumentKind `json:"kind"`
	FileName    string            `json:"fileName"`
	FrontMatter AgentFrontMatter  `json:"frontMatter"`
	Content     string            `json:"content"`
	ByteSize    int64             `json:"byteSize"` // 原始文件字节数,用于缓存指纹与日志

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
