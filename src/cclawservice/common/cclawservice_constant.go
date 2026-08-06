package common

import (
	"my.favorite/platformutility/webbase"
	"my.favorite/platformutility/webbase/middleware/basic"
)

// 定义业务级的错误常量和错误信息

const (
	ConstErrorNone = iota + basic.ConstErrorMax + 1000
	ConstErrorLoginParameter
	ConstErrorUserNotExist
	ConstErrorNonToken
	ConstErrorInvalidToken
)

const (
	ConstErrorNoneMsg           = "success"
	ConstErrorLoginParameterMsg = "登录参数错误"
	ConstErrorUserNotExistMsg   = "用户不存在"
	ConstErrorNonTokenMsg       = "Token不存在,请登录"
	ConstErrorInvalidTokenMsg   = "Token无效,请登录"
)

const (
	RelativeCreateSession = "/createSession"
	CreateSessionUrl      = webbase.BasePath + RelativeCreateSession

	RelativeGetSessions = "/getSessions"
	GetSessionsUrl      = webbase.BasePath + RelativeGetSessions

	RelativeDeleteSession = "/deleteSession"
	DeleteSessionUrl      = webbase.BasePath + RelativeDeleteSession

	RelativeChatUrl = "/chat"
	ChatUrl         = webbase.BasePath + RelativeChatUrl

	RelativeDeleteChat = "/deleteChat"
	DeleteChatUrl      = webbase.BasePath + RelativeDeleteChat
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
