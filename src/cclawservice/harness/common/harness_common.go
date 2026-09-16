/**
* harness常量，harness全局通用
**/

package common

import (
	"context"
	"strconv"
)

// ==================== agent 启动文档与 skill 文档 ====================

// 启动文档注入预算(安全/性能,应用于 bootstrap-budget)
const (
	// StartupFileCacheTTLSeconds 启动文件列表缓存的 TTL(秒):管理界面可接受短暂延迟,换取高并发下免重复磁盘 IO
	StartupFileCacheTTLSeconds = 30

	MaxBootstrapTotalChars = 60 * 1024  //启动文档注入总上限(OpenClaw bootstrapTotalMaxChars 默认值);单位 **rune**,≈41k token(中文)
	MaxAgentDocSize        = 256 * 1024 //agent 启动文档单文件读取上限:256KB
	MaxBootstrapFileChars  = 20 * 1024  //单个启动文档注入上限(OpenClaw bootstrapMaxChars 默认值);单位 **rune**
	MaxUserDocChars        = 4 * 1024   //USER.md 单独的字符上限;单位 **rune**
)

// MaxPromptSectionLen 由 prompt_service.assembleSystemPrompt 中的 capSections 施加。
//
// 【兑底常量】当前每个 section 都有更紧的来源上限(MaxBootstrapFileChars 20480 rune /
// MaxUserDocChars 4096 rune / MaxSkillsPromptChars 18000 rune),它们都小于本值 ⇒
// 本上限在当前常量下**不会截断任何东西**,只防"将来新增一种没有来源上限的 section"。
// 改这些常量前先看 TestSectionCapIsBackstopOnly。
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

const (
	AgentRoutingLLMEnabled        = true // 是否启用 LLM 智能分类路由(关键词未命中时的兜底路由方式)。
	AgentRoutingLLMTimeoutSeconds = 10   // LLM 分类调用超时(秒),避免拖慢主聊天流程。
	AgentRoutingLLMMaxTokens      = 1024 // LLM 分类输出 token 上限(分类只需返回一个短 AgentId)。
	SessionAgentBindingTTLSeconds = 3600 // 会话→Agent 内存绑定的有效期(秒),过期后重新路由。
	AgentToolLoopMax              = 30   // function-calling 工具循环的最大迭代次数(防止模型反复请求工具导致死循环/超时)。

	DefaultToolReplayMaxRunes  = 1200 // 单条工具结果回放的最大字符数(控制历史体积;完整内容仍在文件里)
	DefaultOutputReserveTokens = 4096 // 为模型输出预留的 token 数

	// DefaultMaxHistoryTokens 历史回放上限的默认值(tokens):历史回放预算取
	// min(窗口预算, 本值)。
	//
	// 为什么需要这个旋钮:在此之前唯一能约束历史回放的量是"为系统提示预留的额度"
	// (t_agent.reverse_token_floor),而它同时被三处消费(系统预留 / 历史回放 / 工具循环),
	// 且与窗口强耦合在 1M 窗口下形同虚设 —— 实测:真实会话 79 条记录也只回放 1.2 万 token,
	// 要到几百上千轮才会碰到窗口。
	//
	// 64k 的量级参考(实测 100001c80d… 那个 22 轮真实会话):回放约 1.2 万 token
	// = 每轮约 540 token(工具结果回放被截到 1200 rune,所以远小于文件体积);
	// 因此 64k 大致对应"百轮量级"的会话开始进入压缩 —— 既不打扰短会话,也不会永不触发。
	DefaultMaxHistoryTokens = 64 * 1024
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
