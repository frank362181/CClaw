package common

import (
	"my.favorite/platformutility/webbase/middleware/basic"
)

const (
	AgentServiceWorkspaceDirName = "workspace"         //agent workspace目录名称
	DefaultAgentConfigFile       = "agentdefault.json" //agent 的默认配置所在的文件
)

// ==================== agent 启动文档与 skill 文档 ====================
const (
	AgentWorkspaceDefaultDirName = "default" //agent 的默认工作区子目录名称

	// 启动文档文件名全集(顺序即注入顺序,参考 OpenClaw WORKSPACE_BOOTSTRAP_FILENAMES)
	AgentDocFileName     = "AGENTS.md"    //agent.md:运行规则(必需)
	SoulDocFileName      = "SOUL.md"      //soul.md:人格/语气/边界(可选)
	IdentityDocFileName  = "IDENTITY.md"  //身份:名称/形象/emoji(可选)
	UserDocFileName      = "USER.md"      //用户模型:偏好/沟通风格(可选)
	ToolsDocFileName     = "TOOLS.md"     //tools.md:工具使用指引(可选)
	BootstrapDocFileName = "BOOTSTRAP.md" //首次运行的引导仪式(仅新工作区)
	MemoryDocFileName    = "MEMORY.md"    //长期记忆文件(存在才注入)
	SkillDocFileName     = "SKILL.md"     //skill.md:技能定义
)

// 启动文档注入预算(安全/性能,参考 OpenClaw bootstrap-budget)
const (
	MaxAgentDocSize        = 256 * 1024 //agent 启动文档单文件读取上限:256KB
	MaxSkillDocSize        = 256 * 1024 //skill 文档单文件读取上限:256KB
	MaxBootstrapFileChars  = 20000      //单个启动文档注入上限(OpenClaw bootstrapMaxChars 默认值)
	MaxBootstrapTotalChars = 60000      //启动文档注入总上限(OpenClaw bootstrapTotalMaxChars 默认值)
	MaxUserDocChars        = 4000       //USER.md 单独的字符上限
	MaxPromptSectionLen    = 32 * 1024  //system prompt 单个 section 的最大 rune 数
)

// skill 元数据目录预算(参考 OpenClaw skill-prompt-limits)
const (
	MaxSkillsInPrompt    = 150   //注入 prompt 的 skill 数量上限
	MaxSkillsPromptChars = 18000 //skill 元数据目录的字符上限
)

// 缺失文件标记(OpenClaw context-file: [MISSING] Expected at: <path>)
const MissingFileMarker = "[MISSING]"

// 定义业务级的错误常量和错误信息

const (
	ConstErrorLoginParameter = iota + basic.ConstErrorMax + 1000 + 1
	ConstErrorUserNamePasswordUnmatch
	ConstErrorUserNotExist
	ConstErrorUserDisabled
	ConstErrorNonToken
	ConstErrorInvalidToken
)

const (
	ConstErrorLoginParameterMsg          = "登录参数错误"
	ConstErrorUserNamePasswordUnmatchMsg = "用户名或密码错误"
	ConstErrorUserNotExistMsg            = "用户不存在"
	ConstErrorUserDisabledMsg            = "用户被禁用,请联系管理员"
	ConstErrorNonTokenMsg                = "Token不存在,请登录"
	ConstErrorInvalidTokenMsg            = "Token无效,请登录"
)

// ContextAuthenticatedUser is the gin.Context key for the server-verified user.
const ContextAuthenticatedUser = "cclaw.authenticatedUser"

const (
	BasePath = "/api"

	RelativeHealthUrl = "/heartbeat"
	RelativeLoginUrl  = "/login"
	RelativeLogoutUrl = "/logout"

	HealthUrl = BasePath + RelativeHealthUrl
	LoginUrl  = BasePath + RelativeLoginUrl
	LogoutUrl = BasePath + RelativeLogoutUrl

	RelativeCreateSession = "/createSession"
	CreateSessionUrl      = BasePath + RelativeCreateSession

	RelativeGetSessions = "/getSessions"
	GetSessionsUrl      = BasePath + RelativeGetSessions

	RelativeDeleteSession = "/deleteSession"
	DeleteSessionUrl      = BasePath + RelativeDeleteSession

	RelativeChatUrl = "/chat"
	ChatUrl         = BasePath + RelativeChatUrl

	RelativeDeleteChat = "/deleteChat"
	DeleteChatUrl      = BasePath + RelativeDeleteChat
)

// ================ chat type ================
const (
	ChatTypePrivate = 0 //私聊
	ChatTypeGroup   = 1 //群聊
)

// ==================== channel type ====================
const (
	ChatChannelWeb      = 1 //WEB端
	ChatChannelFeishu   = 2 //飞书端
	ChatChannelWechat   = 3 //微信端
	ChatChannelDingTalk = 4 //钉钉端
)
