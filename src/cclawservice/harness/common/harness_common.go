/**
* 定义context manager所需的变量
**/

package common

import (
	"encoding/json"
	"strings"

	"my.favorite/cclawservice/harness/api/llmproto"
	pCommon "my.favorite/platformutility/common"
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

	DistillMaxFacts    = 8 // 单次蒸馏最多写入的条目数(避免一次会话灌满长期记忆)
	DistillMaxAddFacts = 8 //追加模式下单次最多追加的条目数

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

	DistillMergeTriggerFacts   = 16 // 自动区条目数达到该值时,该用"合并(整体重写)"模式
	DistillMergeTriggerPercent = 80 // 自动区体积达到 DistillMemoryDocMaxChars时触发合并
)

// 消息角色
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
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

/**
* SKILL.md 技能文档的数据对象
*
* SKILL.md 的 YAML Front Matter(元数据)字段与 agentos/agent_skills 的 SkillMetadata 保持一致:
* name / description / user-invocable / disable-mode-invocation / emoji / home_page / os / requires
**/

// SkillRequires 是 SKILL 运行所需的依赖环境
type SkillRequires struct {
	Bins    []string `json:"bins,omitempty" yaml:"bins,omitempty"`
	AnyBins []string `json:"anyBins,omitempty" yaml:"any_bins,omitempty"`
	Env     []string `json:"env,omitempty" yaml:"env,omitempty"`
	Config  []string `json:"config,omitempty" yaml:"config,omitempty"`
}

// SkillFrontMatter 是 SKILL.md 的 YAML 元数据部分
type SkillFrontMatter struct {
	Name                  string        `json:"name,omitempty" yaml:"name,omitempty"`
	Description           string        `json:"description,omitempty" yaml:"description,omitempty"`
	UserInvocable         bool          `json:"userInvocable,omitempty" yaml:"user-invocable,omitempty"`
	DisableModeInvocation bool          `json:"disableModeInvocation,omitempty" yaml:"disable-mode-invocation,omitempty"`
	Emoji                 string        `json:"emoji,omitempty" yaml:"emoji,omitempty"`
	HomePage              string        `json:"home-page,omitempty" yaml:"home-page,omitempty"`
	OS                    []string      `json:"os,omitempty" yaml:"os,omitempty"`
	Version               string        `json:"version,omitempty" yaml:"version,omitempty"`
	Requires              SkillRequires `json:"requires,omitempty" yaml:"requires,omitempty"`
	AllowedTools          []string      `json:"allowed-tools,omitempty" yaml:"allowed-tools,omitempty"`
	DisallowedTools       []string      `json:"disallowed-tools,omitempty" yaml:"disallowed-tools,omitempty"`

	TriggerKeywords []string `json:"triggerKeywords,omitempty" yaml:"trigger-keywords,omitempty"`

	Tools []BasicTool `json:"tools,omitempty" yaml:"tools,omitempty"`
}

// SkillDocument 表示一个已解析的 SKILL.md 文档
type SkillDocument struct {
	SkillId   string `json:"skillId"`   // 技能目录名(如 weather / npa-report)
	SkillFile string `json:"skillFile"` // SKILL.md 的完整路径

	// Fingerprint 读盘时的文件指纹(size:mtime),属**缓存簿记**:内容未变即直接复用本文档。
	// 读盘与 YAML 解析的唯一入口是 PromptRepository.LoadSkills(工具层只消费本文档)。
	Fingerprint string `json:"fingerprint,omitempty"`

	FrontMatter SkillFrontMatter `json:"frontMatter"`
	Content     string           `json:"content"`
	ByteSize    int64            `json:"byteSize"`
}

// ToJsonString 序列化为 JSON 字符串
func (doc *SkillDocument) ToJsonString() (string, error) {
	jsonStr, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}

	return string(jsonStr), nil
}

// FromJsonString 从 JSON 字符串反序列化
func (doc *SkillDocument) FromJsonString(jsonStr string) error {
	return json.Unmarshal([]byte(jsonStr), doc)
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

	//注入到prompt的动态区
	DynamicMemory string `json:"dynamicMemory,omitempty"`

	// 本轮命中的技能正文(规程),与DynamicMemory一样后置 注入
	DynamicSkills string `json:"dynamicSkills,omitempty"`

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

type SystemToolSchemaMeta struct {
	Name        string         `json:"name,omitempty" yaml:"name,omitempty"`
	Description string         `json:"description,omitempty" yaml:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty" yaml:"input_schema,omitempty"`
}

// SystemToolSchemaDocument 系统工具的schema,一般是JSON文件
type SystemToolSchemaDocument struct {
	ToolName          string `json:"toolName,omitempty" yaml:"toolName,omitempty"`
	Visible           bool   `json:"visible,omitempty" yaml:"visible,omitempty"`
	Script            string `json:"script,omitempty" yaml:"script,omitempty"`
	SchemaFileName    string `json:"schemaFileName,omitempty" yaml:"schemaFileName,omitempty"`
	SchemaFingerprint string `json:"schemaFingerprint,omitempty" yaml:"schemaFingerprint,omitempty"`
	ByteSize          int64  `json:"byteSize,omitempty" yaml:"byteSize,omitempty"`

	SysToolSchemaMeta SystemToolSchemaMeta `json:"systemToolSchemaMeta,omitempty" yaml:"systemToolSchemaMeta,omitempty"`
}

type TSource int // tool的来源
const (
	TSourceSystem TSource = iota // 基础工具
	TSourceSkill                 //来自于plugin的工具
	TSourcePlugin
)

const (
	TSourceUnknown    string = "unknown"
	TSourceSystemName        = "system"
	TSourceSkillName         = "skill"
	TSourcePluginName        = "plugin"
)

func (src TSource) String() string {
	switch src {
	case TSourceSystem:
		return TSourceSystemName
	case TSourceSkill:
		return TSourceSkillName
	case TSourcePlugin:
		return TSourcePluginName
	default:
		return TSourceUnknown
	}
}

// BasicTool 是 SKILL.md front matter/系统工具的json schema 中声明的一个可调用工具(供 function-calling 注册)。
//   - Name/Description/Parameters:用于生成给 LLM 的函数 schema;
//   - Script:脚本路径。front matter 里声明的是相对 SKILL.md 所在目录的路径(如 scripts/weather.py);
//     派生层(tool repository)会解析成**绝对路径**后才下发,执行时以参数 JSON 写入 stdin,
//     脚本把结果(JSON/文本)写到 stdout,退出码非 0 表示失败(stderr 为错误信息;
//     若脚本只把 {"error":"..."} 写到 stdout,执行器会回退解析 stdout 作为失败原因)。
//   - ForceCall:"必须调用"标记(如取实时数据的工具):工具循环首轮会强制 tool_choice=required。
type BasicTool struct {
	Source TSource

	Name        string         `json:"name,omitempty" yaml:"name,omitempty"`
	Description string         `json:"description,omitempty" yaml:"description,omitempty"`
	Script      string         `json:"script,omitempty" yaml:"script,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	ForceCall   bool           `json:"forceCall,omitempty" yaml:"force-call,omitempty"`
}

type SystemicTool struct {
	BasicTool

	Tool              BasicTool
	SchemaFileName    string //对应的schema input所对应的文件
	SchemaFingerprint string //对应的schema文件的fingerprint
}

func (st *SystemicTool) GetSchemaFileName() string {
	return st.SchemaFileName
}

func (st *SystemicTool) GetSchemaFingerprint() string {
	return st.SchemaFingerprint
}

// ToolPromptItem system prompt 里"本轮可调用工具"清单的一项(**渲染输入**,不是工具定义本身)
type ToolPromptItem struct {
	Name        string  // 工具名(与本轮 request.tools 的 function.name 严格同源)
	Description string  // 一句话用途(取自同一份 schema 的 description)
	Source      TSource // 来源:技能工具 / 系统工具
	SkillId     string  // 归属技能目录名(系统工具为空)
}

// OwnerTag 渲染归属标签:技能工具给"技能 <skill_dir>",系统工具给"系统工具";都判定不了时返回空串。
func (t ToolPromptItem) OwnerTag() string {
	if skillId := strings.TrimSpace(t.SkillId); len(skillId) > 0 {
		return "技能 " + skillId
	}

	if t.Source == TSourceSystem {
		return "系统工具"
	}

	return pCommon.BlankString
}
