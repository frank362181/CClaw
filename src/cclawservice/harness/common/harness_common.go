/**
* harness常量，harness全局通用
**/

package common

import (
	"my.favorite/cclawservice/model/dataobject"
)

// ==================== agent 启动文档与 skill 文档 ====================

// MissingFileMarker 缺失文件标记(OpenClaw context-file: [MISSING] Expected at: <path>)
const MissingFileMarker = "[MISSING]"

const (
	AgentDocFileName     = "AGENTS.md"    //agent.md:运行规则(必需)
	SoulDocFileName      = "SOUL.md"      //soul.md:人格/语气/边界(可选)
	IdentityDocFileName  = "IDENTITY.md"  //身份:名称/形象/emoji(可选)
	UserDocFileName      = "USER.md"      //用户模型:偏好/沟通风格(可选)
	ToolsDocFileName     = "TOOLS.md"     //tools.md:工具使用指引(可选)
	BootstrapDocFileName = "BOOTSTRAP.md" //首次运行的引导仪式(仅新工作区)
	MemoryDocFileName    = "MEMORY.md"    //长期记忆文件(存在才注入)
)

// StartupDocSpec 启动文档清单中的一项(文件名/种类/是否必需)
type StartupDocSpec struct {
	Kind     dataobject.AgentDocumentKind
	Name     string
	Required bool
}

// StartupDocFiles 启动文档清单,按固定顺序加载。
// required=true 的必需文件缺失时,仍会生成带 Missing 标记的文档,由 prompt 组装层注入 [MISSING] 提示;
// required=false 的可选文件缺失时直接省略。
var StartupDocFiles = []StartupDocSpec{
	{dataobject.AgentDocAgent, AgentDocFileName, true},
	{dataobject.AgentDocSoul, SoulDocFileName, false},
	{dataobject.AgentDocIdentity, IdentityDocFileName, false},
	{dataobject.AgentDocUser, UserDocFileName, false},
	{dataobject.AgentDocTools, ToolsDocFileName, false},
	{dataobject.AgentDocBootstrap, BootstrapDocFileName, false},
	{dataobject.AgentDocMemory, MemoryDocFileName, false},
}

// 启动文档注入预算(安全/性能,应用于 bootstrap-budget)
const (
	// StartupFileCacheTTLSeconds 启动文件列表缓存的 TTL(秒):管理界面可接受短暂延迟,换取高并发下免重复磁盘 IO
	StartupFileCacheTTLSeconds = 30

	MaxAgentDocSize        = 256 * 1024 //agent 启动文档单文件读取上限:256KB
	MaxSkillDocSize        = 256 * 1024 //skill 文档单文件读取上限:256KB
	MaxBootstrapFileChars  = 20000      //单个启动文档注入上限(OpenClaw bootstrapMaxChars 默认值)
	MaxBootstrapTotalChars = 60000      //启动文档注入总上限(OpenClaw bootstrapTotalMaxChars 默认值)
	MaxUserDocChars        = 4000       //USER.md 单独的字符上限
)

// MaxPromptSectionLen 的大小限制
const (
	MaxPromptSectionLen = 32 * 1024 //system prompt 单个 section 的最大 rune 数
)

// skill 元数据目录预算
const (
	SkillDocFileName     = "SKILL.md" //skill.md:技能定义
	MaxSkillsInPrompt    = 150        //注入 prompt 的 skill 数量上限
	MaxSkillsPromptChars = 18000      //skill 元数据目录的字符上限
)

const (
	AgentRoutingLLMEnabled        = true // 是否启用 LLM 智能分类路由(关键词未命中时的兜底路由方式)。
	AgentRoutingLLMTimeoutSeconds = 5    // LLM 分类调用超时(秒),避免拖慢主聊天流程。
	AgentRoutingLLMMaxTokens      = 1024 // LLM 分类输出 token 上限(分类只需返回一个短 AgentId)。
	SessionAgentBindingTTLSeconds = 3600 // 会话→Agent 内存绑定的有效期(秒),过期后重新路由。
	AgentToolLoopMax              = 30   // function-calling 工具循环的最大迭代次数(防止模型反复请求工具导致死循环/超时)。
)

// LLM 调用重试 / 单次请求空闲超时/流式空闲超时 / SSE 心跳相关配置(秒或毫秒;<=0 表示禁用)。
const (
	AgentRunTimeout = 600 //Agent单次聊天运行超时时间(工具循环+LLM生成)

	NonStreamResponseTimeout = 60 // 单次 LLM 同步请求的默认超时，同步情况下起作用
	StreamResponseTimeout    = 60 // 流式读取空闲上限(秒):LLM无返回的最大时间
	SSEStreamHeartbeatSecs   = 15 // SSE 空闲心跳间隔(秒)

	ToolCallingHeartbeatInterval = 3 //工具调用超时时间，unit = second,用于区分是长时间挂起还是工具在执行

	LlmRetryMaxAttempts = 3    // 可重试错误(429/5xx/断流)的最大尝试次数(含首次)
	LlmRetryBaseDelayMs = 500  // 指数退避基准(毫秒):500 → 1000 → 2000 …
	LlmRetryMaxDelayMs  = 4000 // 单次退避上限(毫秒)
	LlmRetryJitterRatio = 0.2  // 退避抖动比例(±20%):打散并发重试,避免惊群

	MaxResponseBytes = 8 << 20 // 单次从response中读取上限(8MB),防止超大响应耗尽内存。
)

// 消息角色
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
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

// ==================== 记忆管理模块(阶段1:双写落库 + 历史文件) ====================
// 对应 data/ddl_sqlite3.sql 的 t_session / t_turn / t_turn_tool 三张表。
// 设计:会话内容写入 memory_{yyyyMMddHHmmss}.md,t_session.chat_data 只保存该文件名,
// 文件目录由 session_id 推导为 <exeDir>/cclaw/chat_history/<session_id>/。
const (
	ChatMemoryDirName    = "chat_history" // 会话历史文件根目录名(与 AgentBootstrapDir 同级)。
	ChatMemoryFilePrefix = "memory_"      // 历史文件名前缀:memory_{yyyyMMddHHmmss}.md
	ChatMemoryFileExt    = ".md"          // 历史文件扩展名。
	SessionNameMaxLen    = 64             // 会话名称长度上限(对齐 t_session.session_name VARCHAR(64))。
	SessionDefaultName   = "NewSession"   // 无可用标题时的会话名称(首条消息为空时兜底)。
	TurnToolRespMaxLen   = 256            //工具输出截取的字符数
)

// TruncatedMarker 截断标记:工具/历史的截断处显式标出省略号。
const TruncatedMarker = "…"

// 压缩模式 safeguard(保全模式,mode=1)**专属**参数
const (
	SafeguardBlockRecords    = 20  // 分块大小(每块单独摘要后拼接)
	SafeguardMinKeyRetention = 0.5 // 关键信息保有率下限
)

// 压缩参数
const (
	CompactionMinRecords         = 4     // 触发压缩的最小记录数(区间太小不值得一次 LLM 调用)
	MaxCompactionInputChars      = 24000 // 送入摘要模型的源文本字符上限(超出按序号从新到旧舍弃)
	MaxSummaryRunes              = 4000  // 摘要记录正文的字符上限(摘要自身也要有预算)
	CompactionRecordExcerptRunes = 2000  // 单条记录送入模型时的最大字符数

	CompactionRunTimeout = 120  //后台压缩的最长时长,因为压缩也是要调用LLM的,超时就放弃,下一轮再来
	SummaryMaxTokens     = 2048 //SummaryMaxTokens,摘要也要有预算
)

// 蒸馏参数
const (
	// DistillMinTurns 蒸馏的触发条件
	DistillMinTurns      = 6  // 触发蒸馏的最少轮次数(太短的会话没有长期价值)
	DistillMinRecords    = 6  // 触发蒸馏的最小记录数(太短的对话没有长期价值)
	DistillIntervalTurns = 10 // 蒸馏间隔(轮次):每跨过这么多个轮次蒸馏一次

	DistillMaxFacts          = 8                     // 单次蒸馏最多写入的条目数(避免一次会话灌满长期记忆)
	DistillMaxFactRunes      = 200                   // 单条事实的最大字符数
	DistillMaxInputChars     = 20000                 // 送入模型的源文本字符上限
	DistillSectionTimeLayout = "2006-01-02 15:04:05" // 小节标题的时间格式
	DistillNoneMark          = "NONE"                // 模型判定"没有值得长期记住的内容"时的输出
)
