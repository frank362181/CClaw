package common

import (
	"time"

	"my.favorite/cclawservice/model/dataobject"
)

// ContextAuthenticatedUser is the gin.Context key for the server-verified user.
const ContextAuthenticatedUser = "cclaw.authenticatedUser"

const (
	OwnerIdPlatform = "000000" //属于平台资产定义

	// DefaultReloadDatabaseCacheTTLSeconds 配置缓存 TTL(多实例部署时,各实例从 DB 刷新 LLM provider/agent/skill 配置的过期时间)
	DefaultReloadDatabaseCacheTTLSeconds = 30 // 默认 30 秒;<=0 表示禁用 TTL 刷新(仅启动时加载一次)

	AgentServiceWorkspaceDirName = "workspace"         //agent工作目录
	AgentBootstrapDir            = "cclaw"             //agent 启动文件目录名称
	DefaultAgentConfigFile       = "agentdefault.json" //agent 的默认配置所在的文件

	AgentExtensionDirName = "extension"
)

// ==================== agent 根据内容意图识别确定agent ====================

const (
	// AgentRoutingLLMEnabled 是否启用 LLM 智能分类路由(关键词未命中时的兜底路由方式)。
	AgentRoutingLLMEnabled = true
	// AgentRoutingLLMTimeoutSeconds LLM 分类调用超时(秒),避免拖慢主聊天流程。
	AgentRoutingLLMTimeoutSeconds = 5
	// AgentRoutingLLMMaxTokens LLM 分类输出 token 上限(分类只需返回一个短 AgentId)。
	AgentRoutingLLMMaxTokens = 1024
	// SessionAgentBindingTTLSeconds 会话→Agent 内存绑定的有效期(秒),过期后重新路由。
	SessionAgentBindingTTLSeconds = 3600
	// AgentToolLoopMax function-calling 工具循环的最大迭代次数(防止模型反复请求工具导致死循环/超时)。
	AgentToolLoopMax = 30

	// DefaultMaxChatConcurrency 全局聊天并发闸默认上限:每实例同时在聊天请求数(同步+流式)。
	DefaultMaxChatConcurrency = 1000

	// DefaultMaxStreamConcurrency SSE 流并发闸默认上限:每实例的流式连接数。
	DefaultMaxStreamConcurrency = 800 // DefaultAgentConcurrency

	// DefaultAgentConcurrency 每 Agent 并发闸默认上限:t_agent.max_concurrent<=0 时采用。
	DefaultAgentConcurrency = 8
	// AgentGateWaitTimeout 获取每 Agent 并发许可的最长等待;超时返回 "Agent 忙"。
	AgentGateWaitTimeout = 30
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

// 启动文档注入预算(安全/性能,参考 OpenClaw bootstrap-budget)
const (
	// StartupFileCacheTTLSeconds 启动文件列表缓存的 TTL(秒):管理界面可接受短暂延迟,换取高并发下免重复磁盘 IO
	StartupFileCacheTTLSeconds = 30

	MaxAgentDocSize        = 256 * 1024 //agent 启动文档单文件读取上限:256KB
	MaxSkillDocSize        = 256 * 1024 //skill 文档单文件读取上限:256KB
	MaxBootstrapFileChars  = 20000      //单个启动文档注入上限(OpenClaw bootstrapMaxChars 默认值)
	MaxBootstrapTotalChars = 60000      //启动文档注入总上限(OpenClaw bootstrapTotalMaxChars 默认值)
	MaxUserDocChars        = 4000       //USER.md 单独的字符上限
)

// skill 元数据目录预算

const (
	SkillDocFileName     = "SKILL.md" //skill.md:技能定义
	MaxSkillsInPrompt    = 150        //注入 prompt 的 skill 数量上限
	MaxSkillsPromptChars = 18000      //skill 元数据目录的字符上限
)

// system prompt的大小限制

const (
	MaxPromptSectionLen = 32 * 1024 //system prompt 单个 section 的最大 rune 数
)

// LLM 调用重试 / 流式空闲超时 / SSE 心跳相关配置(秒或毫秒;<=0 表示禁用)。
const (
	LlmRetryMaxAttempts      = 3    // 可重试错误(429/5xx/断流)的最大尝试次数(含首次)
	LlmRetryBaseDelayMs      = 500  // 指数退避基准(毫秒):500 → 1000 → 2000 …
	LlmRetryMaxDelayMs       = 4000 // 单次退避上限(毫秒)
	LlmRetryJitterRatio      = 0.2  // 退避抖动比例(±20%):打散并发重试,避免惊群
	LlmStreamIdleTimeoutSecs = 60   // 流式读取空闲上限(秒):超过无任何数据则判定断流并中止
	ChatStreamHeartbeatSecs  = 15   // SSE 空闲心跳间隔(秒):静默超时向浏览器发送心跳保活
)

const (
	BasePath = "/api"

	RelativeHealthUrl = "/heartbeat"
	RelativeLoginUrl  = "/login"
	RelativeLogoutUrl = "/logout"

	RelativeGetLLMProviders   = "/llmProviders"
	RelativeAddLLMProvider    = "/llmProvider/add"
	RelativeUpdateLLMProvider = "/llmProvider/update"
	RelativeRemoveLLMProvider = "/llmProvider/remove"

	RelativeGetAgents   = "/agents"
	RelativeAddAgent    = "/agent/add"
	RelativeUpdateAgent = "/agent/update"
	RelativeRemoveAgent = "/agent/remove"

	RelativeGetAgentStartupFiles = "/agent/startupFiles"
	RelativeGetAgentStartupFile  = "/agent/startupFile"

	RelativeGetSkills   = "/skills"
	RelativeAddSkill    = "/skill/add"
	RelativeUpdateSkill = "/skill/update"
	RelativeRemoveSkill = "/skill/remove"

	RelativeChatUrl          = "/chat"
	RelativeChatStreamUrl    = "/chat/stream"
	RelativeChatInterruptUrl = "/chat/interrupt"
)

// ================ chat type ================
const (
	ChatTypePrivate = 0 //私聊
	ChatTypeGroup   = 1 //群聊
)

// ==================== channel type ====================
const (
	ChatChannelWeb      = 1 //WEB端
	ChatChannelFeiShu   = 2 //飞书端
	ChatChannelWechat   = 3 //微信端
	ChatChannelDingTalk = 4 //钉钉端
)

// ==================== chat 流式状态 ====================
// 流式接口在返回内容前,用 status 事件向 UI 报告 Agent 当前所处阶段(如拆解任务/思考/推理)。
const (
	ChatStatusPreparing = "preparing" //解析任务/准备上下文
	ChatStatusThinking  = "thinking"  //思考/推理中(等待首个 token)

	// 工具调用过程中的阶段(tool 事件的 phase 值,下发前端;前端按此渲染状态,不可改名)
	ChatToolEventIntent   = "intent"   // 模型声明将调用工具
	ChatToolEventRunning  = "running"  // 开始执行
	ChatToolEventProgress = "progress" // 执行过程输出
	ChatToolEventDone     = "done"     // 执行成功
	ChatToolEventError    = "error"    // 执行失败
)

// LLM 调用关联的constants
const (
	// MaxResponseBytes 单次应答读取上限(8MB),防止超大响应耗尽内存。
	MaxResponseBytes = 8 << 20

	// DefaultTimeout 单次 LLM 请求的默认超时。
	// 真实模型推理(大上下文 + 大 max_tokens)可能耗时较长,10s 过短会在读响应时因
	// 上下文被取消(客户端断开/超时)而中断;这里放宽到 60s(需与服务端 writeTimeout、前端超时对齐)。
	DefaultTimeout = 60 * time.Second
)

// 以独立 notice 事件下发,前端单独展示为警示行,不覆盖回答正文。
const (
	ChatNoticeTruncated     = "truncated"      // finish_reason=length:回答达到输出长度上限
	ChatNoticeContentFilter = "content_filter" // finish_reason=content_filter:回答被内容安全策略拦截
	ChatNoticeIncomplete    = "incomplete"     // 流中途中断(已输出部分内容):回答可能不完整

	ConstChatNoticeTruncatedMsg     = "回答已达长度上限,可能不完整"
	ConstChatNoticeContentFilterMsg = "回答未通过模型内容安全审核,可能不完整"
	ConstChatNoticeIncompleteMsg    = "回答生成中断,可能不完整"
)
