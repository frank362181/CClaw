/**
* System Prompt 的数据对象
*
* 面向单个 agent,由启动文档与 skill 元数据组装而成。
**/

package dataobject

import (
	"encoding/json"
)

// BootstrapScope 定义启动文档注入的会话范围。
// filterBootstrapFilesForSession:不同会话形态注入不同的启动文件,
// 并据此控制 MEMORY.md 等隐私敏感文件的可见性。
type BootstrapScope int

// 定义启动范围，决定哪些信息进入到system prompt
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

// SystemPromptSection 是 system prompt 的一个组成部分(标题 + 内容)
type SystemPromptSection struct {
	Title   string `json:"title,omitempty"`
	Content string `json:"content"`
}

// SystemPrompt 是组装好的 system prompt
type SystemPrompt struct {
	AgentId   string                `json:"agentId"`
	Workspace string                `json:"workspace"`
	Scope     BootstrapScope        `json:"scope"`
	Sections  []SystemPromptSection `json:"sections"`
	Prompt    string                `json:"prompt"`
	Length    int                   `json:"length"` // prompt 的字节长度

	Missing   []string `json:"missing,omitempty"`   // 必需启动文档缺失的清单
	Truncated []string `json:"truncated,omitempty"` // 因预算被截断的文件清单
}

// ToJsonString 序列化为 JSON 字符串
func (prompt *SystemPrompt) ToJsonString() (string, error) {
	jsonStr, err := json.Marshal(prompt)
	if err != nil {
		return "", err
	}

	return string(jsonStr), nil
}

// FromJsonString 从 JSON 字符串反序列化
func (prompt *SystemPrompt) FromJsonString(jsonStr string) error {
	return json.Unmarshal([]byte(jsonStr), prompt)
}
