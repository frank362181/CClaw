package exception

import (
	pException "my.favorite/platformutility/exception"
)

// 定义每个error类型的编码段
const (
	codeErrorUserBase          = pException.ErrorIdSystemBase + 1000
	codeErrorDatabase          = pException.ErrorIdSystemBase + 1400
	codeErrorAgentBase         = pException.ErrorIdSystemBase + 2000
	codeErrorRuntimeClientBase = pException.ErrorIdSystemBase + 2500
	codeErrorHarnessChatBase   = pException.ErrorIdSystemBase + 2600
)
