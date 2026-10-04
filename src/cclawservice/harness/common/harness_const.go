/**
* harness常量，harness全局通用
**/

package common

import (
	"context"
	"strconv"
)

// ==================== agent 启动文档与 skill 文档 ====================

const (
	AgentBootstrapDir = "cclaw" //系统 启动文件目录名称

	AgentServiceWorkspaceDirName = "workspace"         //agent工作目录
	DefaultAgentConfigFile       = "agentdefault.json" //agent 的默认配置所在的文件

	SystemToolDirRoot = "systemtool"
	SkillDirRoot      = "skills" //skills 的目录
)

// AgentDocumentKind 启动文档种类(实体词汇:AgentDocument.Kind 的取值)。
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

// 启动文档文件名(*DocFileName 属于"工作区布局配置",留在本包;
// 文档种类 AgentDocumentKind 是实体词汇,定义在 model/dataobject)
const (
	AgentDocFileName     = "AGENTS.md"    //agent.md:运行规则(必需)
	SoulDocFileName      = "SOUL.md"      //soul.md:人格/语气/边界(可选)
	IdentityDocFileName  = "IDENTITY.md"  //身份:名称/形象/emoji(可选)
	UserDocFileName      = "USER.md"      //用户模型:偏好/沟通风格(可选)
	ToolsDocFileName     = "TOOLS.md"     //tools.md:工具使用指引(可选)
	BootstrapDocFileName = "BOOTSTRAP.md" //首次运行的引导仪式(仅新工作区)
	MemoryDocFileName    = "MEMORY.md"    //长期记忆文件(存在才注入)
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

// MissingFileMarker 缺失文件标记(OpenClaw context-file: [MISSING] Expected at: <path>)
const MissingFileMarker = "[MISSING]"

// 启动文档注入预算(安全/性能,应用于 bootstrap-budget)
const (
	// StartupFileCacheTTLSeconds 启动文件列表缓存的 TTL(秒):管理界面可接受短暂延迟,换取高并发下免重复磁盘 IO
	StartupFileCacheTTLSeconds = 30

	MaxBootstrapTotalChars = 60 * 1024  //启动文档注入总上限(OpenClaw bootstrapTotalMaxChars 默认值);单位 **rune**,≈41k token(中文)
	MaxAgentDocSize        = 256 * 1024 //agent 启动文档单文件读取上限:256KB
	MaxBootstrapFileChars  = 20 * 1024  //单个启动文档注入上限(OpenClaw bootstrapMaxChars 默认值);单位 **rune**
	MaxUserDocChars        = 4 * 1024   //USER.md 单独的字符上限;单位 **rune**
)

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

// LLM 调用重试 / 单次请求空闲超时/流式空闲超时 / SSE 心跳相关配置(秒或毫秒;<=0 表示禁用)。
const (
	AgentRunTimeout  = 600                          //Agent单次聊天运行超时时间(工具循环+LLM生成)
	ChatRunTimeout   = "CCLAW_CHAT_RUN_TIMEOUT_SEC" //聊天最长的时间环境变量
	AgentToolLoopMax = 80                           // function-calling 工具循环的最大迭代次数(防止模型反复请求工具导致死循环/超时)。

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

// 压缩参数
const (
	CompactionMinRecords    = 4     // 触发压缩的最小记录数(区间太小不值得一次 LLM 调用)
	MaxCompactionInputChars = 24000 // 送入摘要模型的源文本字符上限:放不下时**收缩覆盖区间**(绝不丢弃区间内的记录,见 fitCompactionRange)
	MaxSummaryRunes         = 4000  // 摘要记录正文的字符上限(摘要自身也要有预算)

	// SummaryShrinkRatio 压缩收益门槛:压缩的价值来自"变小",因此摘要上限随源体积收缩,源太小时直接跳过。
	//   有效上限 = clamp(源 runes * SummaryShrinkRatio, SummaryMinRunes, MaxSummaryRunes)
	//   源 runes <= 有效上限 → 压缩没有净收益 → 不发起 LLM 调用。
	SummaryShrinkRatio = 0.6 // 摘要不超过源体积的比例
	SummaryMinRunes    = 400 // 摘要的下限(再小就没有信息量)

	// MinCompactionSrcRunes 触发压缩的**体积**门槛:被裁区间至少这么大才值得一次 LLM 调用。
	MinCompactionSrcRunes = SummaryMinRunes

	// MaxSummaryTotalRunes 注入 system 的历史摘要总量上限。
	MaxSummaryTotalRunes = 12000

	CompactionRunTimeout = 120 //后台压缩的最长时长,因为压缩也是要调用LLM的,超时就放弃,下一轮再来

	// SummaryMaxTokens 摘要/长期记忆这类"维护调用"的输出 token 上限。
	// 必须 ≥ 代码自己承诺的长度上限,否则契约与能力自相矛盾:
	//   - 落盘上限 MaxSummaryRunes(4000 rune)≈ 2667 token(中文按 1.5 字符/token 估);
	//   - 蒸馏要求模型回"完整列表" —— DistillMaxMemoryFacts(20) × DistillMaxFactRunes(200) = 4000 rune。
	SummaryMaxTokens = 4096

	// MaintenanceMinOutputTokens 维护调用(蒸馏/压缩/合并)的**最小**输出额度。
	// 窗口扣掉输入后剩余不到这个数时,调用方直接跳过 —— 输出这么短必然是一段被截断的残文,
	// 写进历史摘要或长期记忆只会造成信息缺口,不如不写(问题指向窗口/输出额度配置)。
	MaintenanceMinOutputTokens = 512

	// CompactionMaxConcurrent 后台记忆维护(压缩/蒸馏)的全局并发上限:
	// 记忆是旁路增强,不应把上游 RPM/TPM 配额吃光、挤掉用户真正的对话请求。
	CompactionMaxConcurrent = 3
	// CompactionMergeMinSummaries 触发"摘要合并"所需的最少摘要条数(只合并不了 1 条)。
	CompactionMergeMinSummaries = 2
)

// 蒸馏参数
const (
	// DistillMinTurns 蒸馏的触发条件
	DistillMinTurns      = 6  // 触发蒸馏的最少轮次数(太短的会话没有长期价值)
	DistillMinRecords    = 6  // **首次**蒸馏的最小记录数(太短的对话没有长期价值)
	DistillIntervalTurns = 10 // 蒸馏节拍(轮次):作为**速率上限**,每跨过这么多轮蒸馏一次

	// DistillCheckIntervalTurns 内容驱动触发的最短间隔(轮)。
	// 节拍是速率上限,内容才是触发源:水位之后攒够一轮送入门槛(DistillMaxInputChars)
	// 就提前排空 —— 工具密集的会话单轮就能产出远超一次送入预算的记录,
	// 只靠节拍会让队列无限增长(实测 Backlog 只会打日志、从来不调速)。
	// 按固定短间隔生效(而不是每轮),避免蒸馏持续失败时每轮重试同一份输入。
	DistillCheckIntervalTurns = 3

	// DistillMemoryDocMaxChars MEMORY.md 的体积上限(蒸馏写入时裁剪)。
	//
	// 刻意**小于**单文档注入上限 MaxBootstrapFileChars:MEMORY.md 在启动文档注入顺序
	// (AGENTS → SOUL → … → BOOTSTRAP → MEMORY)里是**最后**一段,总预算
	// (enforceTotalBudget)吃紧时它是第一个被截断/丢弃的 —— 写满到单文档上限,
	// 就会出现"事实写进了文件、却从没进过 prompt,同时为了这个预算把旧事实删了"。
	DistillMemoryDocMaxChars = 12 * 1024

	// DistillSimilarFactRatio / DistillSimilarFactMinRunes "新事实覆盖旧事实"的判定阈值:
	// 两条事实的字符二元组 Jaccard 相似度 ≥ 阈值、且都不短于下限时,视为同一槽位的更新
	// (例:"单位编码固定为 100001" → "单位编码固定为 100002")。
	// 只做**同义改写/取值更新**这一层,语义级去重本地做不到,不假装能做。
	DistillSimilarFactRatio    = 0.6
	DistillSimilarFactMinRunes = 10

	DistillMaxFacts = 8 // 单次蒸馏最多写入的条目数(避免一次会话灌满长期记忆)
	// DistillMaxMemoryFacts 自动记忆区**总计**允许保留的条目数上限。
	// 与 DistillMaxFacts 的区别:自动区是**整体重写**的"当前状态列表",
	// 所以这里限制的是列表总长,而 DistillMaxFacts 限制的是单次解析出的条数。
	DistillMaxMemoryFacts = 20
	// DistillMinFactRunes 一条事实的最短长度:短于此的"事实"多半是前言/残句/标签。
	DistillMinFactRunes      = 8
	DistillMaxFactRunes      = 200                   // 单条事实的最大字符数
	DistillMaxInputChars     = 20000                 // 送入模型的源文本字符上限
	DistillSectionTimeLayout = "2006-01-02 15:04:05" // 小节标题的时间格式
	DistillNoneMark          = "NONE"                // 模型判定"没有值得长期记住的内容"时的输出
)
