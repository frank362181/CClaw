/**
* cclawservice 服务级公共词汇:常量 / 文案 / 枚举 + 少量便利函数。
*
* 依赖规矩(防止 import cycle 复发):
*   - 本包允许依赖 model/dataobject(utility.go 的校验/路径函数以实体为入参);
*   - 因此 **dataobject 需要的东西都不能放本包**(尤其是枚举/类型),必须定义在 dataobject 里;
*   - 本包只可再依赖 llmproto 与 platformutility(不得依赖 harness/*、service/*、controller)。
**/

package common

import "my.favorite/cclawservice/model/dataobject"

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

	// ChatNoticeToolLoopGuard 工具死循环守卫首次命中(模型在重复调用工具,已提醒它换策略)。
	// 再次命中会以 chat.tool_loop_guard 错误收尾,不再重复下发本提示。
	ChatNoticeToolLoopGuard = "tool_loop_guard"

	// ChatNoticeLevelError 通知级别:错误级(前端用醒目样式渲染,如红色图标与描边)。
	ChatNoticeLevelError = "error"

	ConstChatNoticeTruncatedMsg     = "回答已达长度上限,可能不完整"
	ConstChatNoticeContentFilterMsg = "回答未通过模型内容安全审核,可能不完整"
	ConstChatNoticeIncompleteMsg    = "回答生成中断,可能不完整"

	// ConstChatNoticeToolLoopGuardWarnMsg 工具死循环守卫命中提示(按判定级别取话术,见 security.LoopGuardLevel)
	ConstChatNoticeToolLoopGuardWarnMsg = "模型在重复调用工具,已提醒它换一种方式"
	ConstChatNoticeToolLoopGuardStopMsg = "检测到重复调用工具,已要求模型停止重试"
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

// StartupDocSpec 启动文档清单中的一项(文件名/种类/是否必需)
type StartupDocSpec struct {
	Kind     dataobject.AgentDocumentKind `json:"kind"`
	Name     string
	Required bool
}

// MissingFileMarker 缺失文件标记(OpenClaw context-file: [MISSING] Expected at: <path>)
const MissingFileMarker = "[MISSING]"

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

// SKILL的作用域
const (
	SkillScopeSystem = iota
	SkillScopePersonal
	SkillScopeProject
	SkillScopePlugin
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
