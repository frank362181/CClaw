/**
* 定义context manager所需的变量
**/

package common

import (
	"context"
	"encoding/json"
	"strings"

	"my.favorite/cclawservice/harness/api/llmproto"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
)

// StartupDocSpec 启动文档清单中的一项(文件名/种类/是否必需)
type StartupDocSpec struct {
	Kind     AgentDocumentKind `json:"kind"`
	Name     string
	Required bool
}

// StartupDocFiles 启动文档清单,按固定顺序加载。
// required=true 的必需文件缺失时,仍会生成带 Missing 标记的文档,由 prompt 组装层注入 [MISSING] 提示;
// required=false 的可选文件缺失时直接省略。
var StartupDocFiles = []StartupDocSpec{
	{AgentDocAgent, AgentDocFileName, true},
	{AgentDocSoul, SoulDocFileName, false},
	{AgentDocIdentity, IdentityDocFileName, false},
	{AgentDocUser, UserDocFileName, false},
	{AgentDocTools, ToolsDocFileName, false},
	{AgentDocBootstrap, BootstrapDocFileName, false},
	{AgentDocMemory, MemoryDocFileName, false},
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

type ClassifyAgent func(ctx context.Context, llm dataobject.TLlm, system, user string, maxTokens int) (string, string, error)

// ClassificationAgent 是路由分类 prompt 中的单个 Agent 候选。
// AGENTS.md 的 front matter的 description;两者皆空回退到Agent名称。
type ClassificationAgent struct {
	AgentId   string
	AgentName string
	Label     string
}

// ContextPolicy 上下文装配策略(来源:t_agent 与 t_llm 配置)。
type ContextPolicy struct {
	ContextWindow int // 模型上下文窗口(tokens);<=0 表示未知,不做裁剪(保持阶段1行为)
	OutputReserve int // 为输出预留的 tokens;<=0 取默认

	// SystemPromptReserveTokens 系统提示(7 个启动文档 + 折叠进 system 的历史摘要)的最小预留额度。
	SystemPromptReserveTokens int

	// MaxHistoryTokens 历史回放上限:<0 表示不限制;0 表示用默认;
	MaxHistoryTokens int

	// ToolsReserveTokens 工具 schema(随请求单独发送,不占 messages)的 token 预留。
	ToolsReserveTokens int

	ToolReplayMaxRunes int // 单条工具结果回放的最大字符数;<=0 取默认
}

// AssembleResult 装配结果。
type AssembleResult struct {
	Messages []llmproto.ChatMessage // 完整消息序列(system + 历史 + 本轮用户)
	Summary  string                 // 折叠进 system 的历史摘要文本(为空表示无摘要)

	Trimmed            bool // 是否因预算裁剪掉了历史
	DroppedFrom        int  // 被裁掉的记录序号区间起点(闭区间;0 表示无)
	DroppedTo          int  // 被裁掉的记录序号区间终点(闭区间;0 表示无)
	DroppedCount       int  // 被裁掉的历史消息条数(工具结果折叠后的条数)
	DroppedRecordCount int  // 被裁掉的历史**记录**条数(工具记录各算一条) —— 压缩门槛用它
	BudgetExhausted    bool // 历史预算已为 0(窗口扣掉输出预留与系统提示预留后没有剩余)

	// 以下为预算分解(可观测):任何一个都能在日志里一眼看到"历史为什么只剩这么多"。
	SystemReserveTokens int // 实际生效的系统提示预留额度 = max(est(system), SystemPromptReserveTokens)
	ToolsReserveTokens  int // 工具 schema 预留
	HistoryCapTokens    int // 生效的历史回放上限;<=0 表示不限制
	HistoryBudgetTokens int // 最终给历史回放用的 token 预算(已扣本轮用户消息)
	KeptHistoryTokens   int // 实际回放的历史估算 token
}

// defaultContextPolicy 返回默认策略(未配置任何压缩参数时的安全值)。
func defaultContextPolicy() ContextPolicy {
	return ContextPolicy{
		OutputReserve:      DefaultOutputReserveTokens,
		ToolReplayMaxRunes: DefaultToolReplayMaxRunes,
	}
}

// GetContextPolicy 按 Agent 与模型配置推导装配策略。
//   - context_window 来自 t_llm;
//   - reverse_token_floor 是 t_agent 上**为系统提示预留的最小额度**(保护启动文档不被历史挤掉);
//   - max_history_tokens 是 t_agent 上**历史回放上限**(压缩节奏旋钮):
//     0/未配置 → 用 DefaultMaxHistoryTokens;值为负 → 不限制。
func GetContextPolicy(agent dataobject.TAgent, contextWindow int) ContextPolicy {
	policy := defaultContextPolicy()
	policy.ContextWindow = contextWindow
	policy.SystemPromptReserveTokens = agent.ReverseTokenFloor
	policy.MaxHistoryTokens = agent.MaxHistoryTokens

	return policy
}

func NormalizeContextPolicy(policy ContextPolicy) ContextPolicy {
	if policy.OutputReserve <= 0 {
		policy.OutputReserve = DefaultOutputReserveTokens
	}
	if policy.ToolReplayMaxRunes <= 0 {
		policy.ToolReplayMaxRunes = DefaultToolReplayMaxRunes
	}
	if policy.SystemPromptReserveTokens < 0 {
		policy.SystemPromptReserveTokens = 0
	}
	if policy.ToolsReserveTokens < 0 {
		policy.ToolsReserveTokens = 0
	}
	// 历史回放上限:0 = 用默认;负数 = 显式不限制。默认值统一在这里兜 ——
	// 手写的 ContextPolicy(测试、特殊调用方)不会因为漏填而变成"不限制"。
	if policy.MaxHistoryTokens == 0 {
		policy.MaxHistoryTokens = DefaultMaxHistoryTokens
	}

	return policy
}

const (
	historySummaryBlockOpen  = "<历史摘要>"
	historySummaryBlockClose = "</历史摘要>"

	memoryBlockOpen  = "<长期记忆>"
	memoryBlockClose = "</长期记忆>"

	skillBlockOpen  = "<技能规程>"
	skillBlockClose = "</技能规程>"

	currentTimeBlockOpen  = "<当前时间>"
	currentTimeBlockClose = "</当前时间>"
)

// TurnContext 表示每轮注入的 **动态上下文** (放在历史之后、本轮用户消息之前)
// 后置的原因:前缀缓存按最长公共前缀命中 -- system 里只要有一个字符变化即缓存作废
// 顺序固化(越后越易变):历史摘要 -> 长期记忆(蒸馏时变) -> 当前时间(每轮变) -> 用户原话(必变)
type TurnContext struct {
	Summary string //折叠进本轮的"历史摘要"文本
	Memory  string // 长期记忆的自动区,MEMORY.md蒸馏产物
	Skills  string // 本轮命中的技能的正文(规程)
	Now     string //当前时间锚点文本
}

// Render 渲染为本轮用户消息的前置块 + 用户原话
func (tc *TurnContext) Render(userMsg string) string {
	var sb strings.Builder

	// 定义一个临时函数
	writeBlock := func(open, close, body string) {
		if body = strings.TrimSpace(body); body == pCommon.BlankString {
			return
		}

		sb.WriteString(open)
		sb.WriteString("\n")
		sb.WriteString(body)
		sb.WriteString("\n")
		sb.WriteString(close)
		sb.WriteString("\n\n")
	}

	writeBlock(historySummaryBlockOpen, historySummaryBlockClose, tc.Summary)
	writeBlock(memoryBlockOpen, memoryBlockClose, tc.Memory)
	writeBlock(skillBlockOpen, skillBlockClose, tc.Skills)
	writeBlock(currentTimeBlockOpen, currentTimeBlockClose, tc.Now)

	sb.WriteString(userMsg)
	return sb.String()
}
