/**
* System Prompt 的数据对象
*
* 面向单个 agent,由启动文档与 skill 元数据组装而成。
**/

package dataobject

import "encoding/json"

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
