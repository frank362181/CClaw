/**
* cclawservice 服务级公共词汇:常量 / 文案 / 枚举 + 少量便利函数。
*
* 依赖规矩(防止 import cycle 复发):
*   - 本包允许依赖 model/dataobject(utility.go 的校验/路径函数以实体为入参);
*   - 因此 **dataobject 需要的东西都不能放本包**(尤其是枚举/类型),必须定义在 dataobject 里;
*   - 本包只可再依赖 llmproto 与 platformutility(不得依赖 harness/*、service/*、controller)。
**/

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

const (
	TruncatedMarker = "…"
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

	ChatNoticeTruncated     = "truncated"      // finish_reason=length:回答达到输出长度上限
	ChatNoticeContentFilter = "content_filter" // finish_reason=content_filter:回答被内容安全策略拦截
	ChatNoticeIncomplete    = "incomplete"     // 流中途中断(已输出部分内容):回答可能不完整

	// ChatNoticeError 需显式提醒用户的错误(欠费/密钥无效/未开通/内容合规/超时等):以独立
	// 告警提示下发,不覆盖已输出内容,也不降级为"可能不完整";文案取自错误的 Kind.Msg。
	ChatNoticeError           = "chat_error"
	ChatNoticeThinkingTimeout = "thinking_timeout"

	// ChatNoticeToolLoopGuard 工具死循环守卫首次命中(模型在重复调用工具,已提醒它换策略)。
	// 再次命中会以 chat.tool_loop_guard 错误收尾,不再重复下发本提示。
	ChatNoticeToolLoopGuard = "tool_loop_guard"

	// ChatNoticeNotRecorded 本轮对话未被记入会话历史(会话归属校验失败:该会话属于其他用户)。
	// 记忆是旁路、不阻断对话;但"每轮都答得出、历史永远为空"必须让用户看得见,
	// 否则用户与运维都无从排查(见 history-memory-review.md A2)。
	ChatNoticeNotRecorded = "not_recorded"

	// ChatNoticeLevelError 通知级别:错误级(前端用醒目样式渲染,如红色图标与描边)。
	ChatNoticeLevelError = "error"

	ConstChatNoticeTruncatedMsg     = "回答已达长度上限,可能不完整"
	ConstChatNoticeContentFilterMsg = "回答未通过模型内容安全审核,可能不完整"
	ConstChatNoticeIncompleteMsg    = "回答生成中断,可能不完整"

	// ConstChatNoticeNotRecordedMsg 会话归属校验失败的提示话术:说明"这轮不进历史"并给出可行动作。
	ConstChatNoticeNotRecordedMsg = "本轮未记入会话历史(该会话不属于当前用户);如需保留历史,请新建会话"

	// ConstChatNoticeToolLoopGuardWarnMsg 工具死循环守卫命中提示(按判定级别取话术,见 security.LoopGuardLevel)
	ConstChatNoticeToolLoopGuardWarnMsg = "模型在重复调用工具,已提醒它换一种方式"
	ConstChatNoticeToolLoopGuardStopMsg = "检测到重复调用工具,已要求模型停止重试"
	ConstChatNoticeThinkingTimeoutMsg   = "模型思考时间过长,已终止本轮;可换个更具体的问法或稍后重试"
)
