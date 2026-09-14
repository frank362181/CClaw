package common

// 注册用户类型
const (
	AccountCatPlatform   = iota //平台用户
	AccountCatPerson            // 个人用户
	AccountCatEnterprise        //企业用户
)

const (
	DefaultRecordsLimit = 30 // 从数据库读取记录时返回的条数

	// ContextAuthenticatedUser is the gin.Context key for the server-verified user.
	ContextAuthenticatedUser = "cclaw.authenticatedUser"

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
	// DefaultMaxChatConcurrency 全局聊天并发闸默认上限:每实例同时在聊天请求数(同步+流式)。
	DefaultMaxChatConcurrency = 1000

	// DefaultMaxStreamConcurrency SSE 流并发闸默认上限:每实例的流式连接数。
	DefaultMaxStreamConcurrency = 800 // DefaultAgentConcurrency

	// DefaultAgentConcurrency 每 Agent 并发闸默认上限:t_agent.max_concurrent<=0 时采用。
	DefaultAgentConcurrency = 8
	// AgentGateWaitTimeout 获取每 Agent 并发许可的最长等待;超时返回 "Agent 忙"。
	AgentGateWaitTimeout = 30
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

	// ChatToolEventIntent 工具调用过程中的阶段(tool 事件的 phase 值,下发前端;前端按此渲染状态,不可改名)
	ChatToolEventIntent   = "intent"   // 模型声明将调用工具
	ChatToolEventRunning  = "running"  // 开始执行
	ChatToolEventProgress = "progress" // 执行过程输出
	ChatToolEventDone     = "done"     // 执行成功
	ChatToolEventError    = "error"    // 执行失败
)

// 以独立 notice 事件下发,前端单独展示为警示行,不覆盖回答正文。
const (
	ChatNoticeTruncated     = "truncated"      // finish_reason=length:回答达到输出长度上限
	ChatNoticeContentFilter = "content_filter" // finish_reason=content_filter:回答被内容安全策略拦截
	ChatNoticeIncomplete    = "incomplete"     // 流中途中断(已输出部分内容):回答可能不完整

	// ChatNoticeError 需显式提醒用户的错误(欠费/密钥无效/未开通/内容合规/超时等):以独立
	// 告警提示下发,不覆盖已输出内容,也不降级为"可能不完整";文案取自错误的 Kind.Msg。
	ChatNoticeError = "chat_error"

	// ChatNoticeLevelError 通知级别:错误级(前端用醒目样式渲染,如红色图标与描边)。
	ChatNoticeLevelError = "error"

	ConstChatNoticeTruncatedMsg     = "回答已达长度上限,可能不完整"
	ConstChatNoticeContentFilterMsg = "回答未通过模型内容安全审核,可能不完整"
	ConstChatNoticeIncompleteMsg    = "回答生成中断,可能不完整"
)
