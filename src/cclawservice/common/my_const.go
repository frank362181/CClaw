package common

import (
	"time"

	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/platformutility/webbase/middleware/basic"
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

// 定义业务级的错误常量和错误信息

const (
	ConstErrorUserUnAuthorized = iota + basic.ConstErrorMax + 1000 + 1
	ConstErrorLoginParameter
	ConstErrorUserNamePasswordUnmatch
	ConstErrorUserNotExist
	ConstErrorUserDisabled
	ConstErrorNonToken
	ConstErrorInvalidToken

	ConstErrorUnAuthenticated //未被授权

	// ConstErrorAgentParameter agent 管理业务错误(400/404/409)
	ConstErrorAgentParameter = iota + basic.ConstErrorMax + 2000 + 1
	ConstErrorAgentNotFound
	ConstErrorAgentConflict
	ConstErrorAgentWorkspace

	// ConstErrorChatParameter chat 业务错误
	ConstErrorChatParameter = iota + basic.ConstErrorMax + 3000 + 1
	ConstErrorChatAgentNotFound
	ConstErrorChatLLM
	ConstErrorChatCanceled
	ConstErrorChatNotActive
	ConstErrorChatBusy
	ConstErrorChatServerBusy
	ConstErrorChatToolLoop
	ConstErrorChatLLMQuota
	ConstErrorChatRunTimeout
	ConstErrorChatLLMAuth
	ConstErrorChatLLMForbidden
	ConstErrorChatLLMNotFound
	ConstErrorChatLLMInvalidRequest
	ConstErrorChatLLMRateLimit
	ConstErrorChatLLMContentFilter
)

const (
	ConstErrorUserUnAuthorizedMsg        = "用户未认证,请登录"
	ConstErrorLoginParameterMsg          = "登录参数错误"
	ConstErrorUserNamePasswordUnmatchMsg = "用户名或密码错误"
	ConstErrorUserNotExistMsg            = "用户不存在"
	ConstErrorUserDisabledMsg            = "用户被禁用,请联系管理员"
	ConstErrorNonTokenMsg                = "Token不存在,请登录"
	ConstErrorInvalidTokenMsg            = "Token无效,请登录"

	ConstErrorUnAuthenticatedMsg = "用户未授权"

	ConstErrorAgentParameterMsg = "参数错误"
	ConstErrorAgentNotFoundMsg  = "智能体不存在或已被删除"
	ConstErrorAgentConflictMsg  = "智能体已存在或版本冲突"
	ConstErrorAgentWorkspaceMsg = "智能体工作空间不可用"

	ConstErrorChatParameterMsg     = "聊天参数错误"
	ConstErrorChatAgentNotFoundMsg = "聊天目标Agent不存在或已禁用"
	ConstErrorChatLLMMsg           = "智能体引擎暂时不可用,请稍后再试"
	ConstErrorChatCanceledMsg      = "请求已被取消"
	ConstErrorChatNotActiveMsg     = "当前会话没有进行中的聊天"
	ConstErrorChatBusyMsg          = "该智能体当前繁忙,请稍后再试"
	ConstErrorChatServerBusyMsg    = "服务繁忙,请稍后再试"
	ConstErrorChatToolLoopMsg      = "任务步骤过多未能完成,请缩小范围或分批执行后重试"
	ConstErrorChatLLMQuotaMsg      = "模型服务额度不足或已耗尽(可能欠费或套餐用尽),请充值/提升配额后重试"
	ConstErrorChatRunTimeoutMsg    = "任务执行超时,请缩小范围或分批执行后重试"

	// 结构化错误类别对应的可操作提示:把可自行修正的配置类错误与瞬时故障分开
	ConstErrorChatLLMAuthMsg           = "模型服务鉴权失败(API Key 无效或未配置),请检查模型密钥配置后重试"
	ConstErrorChatLLMForbiddenMsg      = "无权访问该模型(未开通或未授权),请在模型服务侧开通或授权后重试"
	ConstErrorChatLLMNotFoundMsg       = "模型不存在或名称配置错误,请检查模型名(model)配置后重试"
	ConstErrorChatLLMInvalidRequestMsg = "请求被模型服务拒绝(参数或上下文不合法),请检查模型与上下文配置后重试"
	ConstErrorChatLLMRateLimitMsg      = "模型服务请求过于频繁,请稍后重试"
	ConstErrorChatLLMContentFilterMsg  = "内容未通过模型安全审核,请修改输入后重试"
)

// 流式回答"收尾提示"事件(code + 展示文案):以独立 notice 事件下发,不污染回答正文。
const (
	ChatNoticeTruncated     = "truncated"      // finish_reason=length:回答达到输出长度上限
	ChatNoticeContentFilter = "content_filter" // finish_reason=content_filter:回答被内容安全策略拦截
	ChatNoticeIncomplete    = "incomplete"     // 流中途中断(已输出部分内容):回答可能不完整

	ConstChatNoticeTruncatedMsg     = "回答已达长度上限,可能不完整"
	ConstChatNoticeContentFilterMsg = "回答未通过模型内容安全审核,可能不完整"
	ConstChatNoticeIncompleteMsg    = "回答生成中断,可能不完整"
)

// LLM 调用重试 / 流式空闲超时 / SSE 心跳相关配置(秒或毫秒;<=0 表示禁用)。
const (
	LlmRetryMaxAttempts      = 3    // 可重试错误(429/5xx/断流)的最大尝试次数(含首次)
	LlmRetryBaseDelayMs      = 500  // 指数退避基准(毫秒):500 → 1000 → 2000 …
	LlmRetryMaxDelayMs       = 4000 // 单次退避上限(毫秒)
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

	// ChatToolEventIntent 工具调用过程中的阶段 */
	ChatToolEventIntent   = "intent"
	ChatToolEventRunning  = "running"
	ChatToolEventProgress = "progress"
	ChatToolEventDone     = "done"
	ChatToolEventError    = "error"
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
