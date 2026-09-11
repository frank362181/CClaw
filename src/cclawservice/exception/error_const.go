package exception

import "my.favorite/platformutility/webbase/middleware/basic"

// 定义每个error类型的编码段
const (
	codeErrorUserBase          = basic.ConstErrorMax + 1000
	codeErrorDatabase          = basic.ConstErrorMax + 1400
	codeErrorAgentBase         = basic.ConstErrorMax + 2000
	codeErrorRuntimeClientBase = basic.ConstErrorMax + 2500
	codeErrorHarnessChatBase   = basic.ConstErrorMax + 2600
)
