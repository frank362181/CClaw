package common

import (
	"my.favorite/platformutility/webbase"
	"my.favorite/platformutility/webbase/middleware/basic"
)

// 定义业务级的错误常量和错误信息

const (
	ConstErrorNone           = iota
	ConstErrorLoginParameter = iota + 1 + basic.ConstErrorMax
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
	UserTokenKeyPrefix = "token_" //以token为key存放的用户信息
	UserInfoKeyPrefix  = "user_"  //以user为key存放的用户信息
)

const (
	MemoryDefaultExpireTime = 1800 //内存数据库中数据的默认过期时间,单位为秒
)

const (
	RelativeNewChatUrl = "/newchat"
	NewChatUrl         = webbase.BasePath + RelativeNewChatUrl

	RelativeDeleteChat = "/deletechat"
	DeleteChatUrl      = webbase.BasePath + RelativeDeleteChat

	RelativeGetSessionList = "/getsessionlist"
	GetSessionListUrl      = webbase.BasePath + RelativeGetSessionList

	RelativeGetSession = "/getsession"
	GetSessionUrl      = webbase.BasePath + RelativeGetSession

	RelativeChatUrl = "/chat"
	ChatUrl         = webbase.BasePath + RelativeChatUrl
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
