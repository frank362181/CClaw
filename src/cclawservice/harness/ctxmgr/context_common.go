package ctxmgr

import (
	"context"
	"strconv"
	"strings"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
)

// StartupFileState 启动文件在磁盘上的状态,决定上层如何注入 prompt。
type StartupFileState int

const (
	// StartupFileMissing 文件不存在。目录与符号链接同样按缺失处理(与 O_NOFOLLOW 语义一致)
	StartupFileMissing StartupFileState = iota
	// StartupFileReady 文件存在且原始正文已读入。
	StartupFileReady
	// StartupFileUnavailable 文件存在但正文不可用(超过 MaxAgentDocSize 或读取失败)。
	// 必须与"缺失"区分:缺失的必需文件注入 [MISSING] 占位(提示待创建),
	// 本状态只告警并省略 —— 文件明明存在,不能向模型谎报它不存在。
	StartupFileUnavailable
)

// StartupFileSnapshotItem 单个启动文件的原始快照项(未做任何 prompt 派生)。
type StartupFileSnapshotItem struct {
	Spec     hCommon.StartupDocSpec // 白名单条目(种类/文件名/是否必需)
	State    StartupFileState
	ByteSize int64
	Content  string // 原始正文(含 front matter);仅 State==StartupFileReady 时有效
	Reason   string // State==StartupFileUnavailable 时的原因(供上层记日志)
}

// StartupSnapshot 一个工作区的启动文件整组快照(供prompt 组装层消费)
//   - Fingerprint 与正文同出一批 stat,不会出现"指纹说没变、正文其实已旧";
//   - Files 携带**原始正文**(未剥离 front matter),派生由 prompt 层负责。
type StartupSnapshot struct {
	Fingerprint string
	Files       []StartupFileSnapshotItem
}

// MaxPromptSectionLen 由 prompt_service.assembleSystemPrompt 中的 capSections 施加
// 表示比如system section,user section等
const (
	MaxPromptSectionLen = 32 * 1024 //system prompt 单个 section 的最大 rune 数
)

// skill 元数据目录预算
const (
	SkillDocFileName     = "SKILL.md" //skill.md:技能定义
	MaxSkillDocSize      = 256 * 1024 //skill 文档单文件读取上限:256KB
	MaxSkillsInPrompt    = 150        //注入 prompt 的 skill 数量上限
	MaxSkillsPromptChars = 18000      //skill 元数据目录的字符上限;单位 **rune**(与其它上限一致)
)

// system prompt 里"本轮可调用工具"清单(Available Tools 段)的预算。
//
// 该段由**本轮实际下发的工具 schema** 派生(见 tool.FunctionBundle.PromptItems),只放
// 名字 + 一句话用途 + 归属技能 —— 完整参数/长描述仍在请求体 tools 里,不在这里重复(否则双倍 token)。
const (
	MaxToolsPromptChars    = 6 * 1024 // 工具清单的字符上限;单位 **rune**。超限先缩短描述,再逐条丢弃尾部
	MaxToolDescPromptRunes = 240      // 单个工具描述的注入上限;单位 **rune**(完整描述见 schema)
)

// 命中技能的正文(SOP)注入预算
const (
	MaxActiveSkillDocRunes = 8 * 1024 //单个命中技能正文注入上限;单位 **rune**
	MaxActiveSkillsPerTurn = 4        //单轮最多注入几个技能的正文(防全命中时吃光预算)
)

const (
	AgentRoutingLLMEnabled        = true // 是否启用 LLM 智能分类路由(关键词未命中时的兜底路由方式)。
	AgentRoutingLLMTimeoutSeconds = 10   // LLM 分类调用超时(秒),避免拖慢主聊天流程。
	AgentRoutingLLMMaxTokens      = 1024 // LLM 分类输出 token 上限(分类只需返回一个短 AgentId)。
	SessionAgentBindingTTLSeconds = 3600 // 会话→Agent 内存绑定的有效期(秒),过期后重新路由。

	DefaultToolReplayMaxRunes  = 1200 // 单条工具结果回放的最大字符数(控制历史体积;完整内容仍在文件里)
	DefaultOutputReserveTokens = 4096 // 为模型输出预留的 token 数

	// DefaultMaxHistoryTokens 历史回放上限的默认值(tokens):历史回放预算取
	// min(窗口预算, 本值)，64K 差不多100轮的会话所需的历史消息
	DefaultMaxHistoryTokens = 64 * 1024
)

// 记录类型 / 标记(与写入端常量保持一致,此处补充阶段2新增的语义)
const (
	HistoryRecordTypeUser        = "user"
	HistoryRecordTypeSystem      = "system"
	HistoryRecordTypeAssistant   = "assistant"
	HistoryRecordTypeTool        = "tool"
	HistoryRecordTypeSummary     = "summary"     //表示该记录为summary,压缩摘要记录
	HistoryRecordTypeInterrupted = "interrupted" //表示为中断标记
)

// 中断语义的两处共享文案(读回装配 + 工具留痕)。
const (
	// InterruptedReplyNotice 回放"被中断的助手回复"时加在正文前的标记。
	// 为什么必须标:中断时写进历史的是**残缺**内容,不标注模型会把它当成完整发言
	// (接着半句话继续说、或以为已经回答过而不再补充)。
	// 压缩/蒸馏侧的 recordLabel 已有 "assistant(被中断)" 语义,装配侧必须补齐同一语义
	InterruptedReplyNotice = "[该轮回复被用户中断,以下为中断前已产出的不完整内容]"

	// TurnToolUnfinishedResult 工具"已发起但没拿到结果"时写进历史与 t_turn_tool 的占位正文。
	// 场景:工具执行中被用户中断/取消/超时。调用**已经发生**(可能已产生副作用),
	// 不能因为它没有结果就从审计记录里消失。
	TurnToolUnfinishedResult = `{"status":"unfinished","note":"tool call started but was interrupted before returning a result"}`
)

// 写历史消息的文件metadata,文件目录由 session_id 推导为 <exeDir>/cclaw/chat_history/<session_id>
const (
	ChatMemoryDirName    = "chat_history" // 会话历史文件根目录名(与 AgentBootstrapDir 同级)。
	ChatMemoryFilePrefix = "memory_"      // 历史文件名前缀:memory_{yyyyMMddHHmmss}.md
	ChatMemoryFileExt    = ".md"          // 历史文件扩展名。
	SessionNameMaxLen    = 64             // 会话名称长度上限(对齐 t_session.session_name VARCHAR(64))。
	SessionDefaultName   = "NewSession"   // 无可用标题时的会话名称(首条消息为空时兜底)。
	TurnToolRespMaxLen   = 256            //工具输出截取的字符数
)

// Summarizer 摘要生成器:由调用方注入(service 层包装 LLM 客户端)。
type Summarizer func(ctx context.Context, systemPrompt, userPrompt string) (string, error)

const (
	CompactionModeDefault   = 0 // 默认模式:把需要压缩的区间整体摘要为一条摘要记录。
	CompactionModeSafeguard = 1 // 保全模式:分块摘要 + 摘要后校验关键信息(路径/结论/待办)是否保留,
)

// CompactionModeName 返回压缩模式的稳定名称(供日志/可观测性使用,不得用于业务判断)。
func CompactionModeName(mode int) string {
	switch mode {
	case CompactionModeDefault:
		return "default"
	case CompactionModeSafeguard:
		return "safeguard"
	default:
		return "unknown(" + strconv.Itoa(mode) + ")"
	}
}

// TruncatedMarker 截断标记:工具/历史的截断处显式标出省略号。
const TruncatedMarker = "…"

// 压缩模式 safeguard(保全模式,mode=1)**专属**参数
const (
	SafeguardBlockRecords    = 20  // 分块大小(每块单独摘要后拼接)
	SafeguardMinKeyRetention = 0.5 // 关键信息保有率下限
)

// 记忆参数(压缩与蒸馏共用)
const (
	// MemoryExcerptRunes 单条记录送入模型的正文上限。
	MemoryExcerptRunes = 2000
)

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

// ClassificationAgent 是路由分类 prompt 中的单个 Agent 候选。
// AGENTS.md 的 front matter的 description;两者皆空回退到Agent名称。
type ClassificationAgent struct {
	AgentId   string
	AgentName string
	Label     string
}

type ClassifyAgent func(ctx context.Context, llm dataobject.TLlm, system, user string, maxTokens int) (string, string, error)

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
