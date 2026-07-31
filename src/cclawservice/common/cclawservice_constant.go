package common

import "my.favorite/platformutility/webbase/middleware/basic"

// 定义业务级的错误常量和错误信息

const (
	ConstErrorNone           = iota
	ConstErrorLoginParameter = iota + 1 + basic.ConstErrorMax
	ConstErrorNonToken
	ConstErrorInvalidToken
)

const (
	ConstErrorNoneMsg           = "success"
	ConstErrorLoginParameterMsg = "登录参数错误"
	ConstErrorNonTokenMsg       = "Token不存在,请登录"
	ConstErrorInvalidTokenMsg   = "Token无效,请登录"
)
