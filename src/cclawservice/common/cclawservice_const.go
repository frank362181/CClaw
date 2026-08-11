package common

import (
	"my.favorite/platformutility/webbase/middleware/basic"
)

const (
	AgentServiceWorkspaceDirName = "workspace" //agent workspace目录名称
)

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
