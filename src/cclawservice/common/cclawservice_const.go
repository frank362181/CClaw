package common

import (
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/platformutility/webbase/middleware/basic"
)

const (
	OwnerIdPlatform = "000000" //属于平台资产定义

	// DefaultReloadDatabaseCacheTTLSeconds 配置缓存 TTL(多实例部署时,各实例从 DB 刷新 LLM provider/agent/skill 配置的过期时间)
	DefaultReloadDatabaseCacheTTLSeconds = 30 // 默认 30 秒;<=0 表示禁用 TTL 刷新(仅启动时加载一次)

	AgentServiceWorkspaceDirName = "workspace"         //agent工作目录
	AgentBootstrapDir            = "cclaw"             //agent 启动文件目录名称
	DefaultAgentConfigFile       = "agentdefault.json" //agent 的默认配置所在的文件

	AgentExtensionDirName = "extension"
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
	MaxPromptSectionLen    = 32 * 1024  //system prompt 单个 section 的最大 rune 数
)

// skill 元数据目录预算

const (
	SkillDocFileName     = "SKILL.md" //skill.md:技能定义
	MaxSkillsInPrompt    = 150        //注入 prompt 的 skill 数量上限
	MaxSkillsPromptChars = 18000      //skill 元数据目录的字符上限
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
	ConstErrorAgentNotFoundMsg  = "记录不存在或已被删除"
	ConstErrorAgentConflictMsg  = "记录已存在或版本冲突"
	ConstErrorAgentWorkspaceMsg = "智能体工作空间不可用"

	ConstErrorChatParameterMsg = "聊天参数错误"
)

// ContextAuthenticatedUser is the gin.Context key for the server-verified user.
const ContextAuthenticatedUser = "cclaw.authenticatedUser"

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

	RelativeChatUrl = "/chat"
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
