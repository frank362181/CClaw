/*
* chat 业务错误定义
* 模式与 error_agent.go / error_prompt.go 保持一致:
*   struct 嵌 exception.ErrorDescription + 哨兵 + Ex/Ex2 工厂 + Error()/Is()
**/

package exception

import (
	"errors"
	"fmt"
	"runtime"

	"my.favorite/platformutility/exception"
)

// ErrorChatParameter 聊天参数错误(HTTP 400)
type ErrorChatParameter struct {
	exception.ErrorDescription
}

var ChatParameterErr = &ErrorChatParameter{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizChatParameter,
		FileName: "",
		LineNo:   -1,
	},
}

func ChatParameterErrEx(errTxt string) *ErrorChatParameter {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatParameter{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizChatParameter,
			FileName: file,
			LineNo:   line,
		},
	}
}

func ChatParameterErrEx2(errTxt string, a ...interface{}) *ErrorChatParameter {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatParameter{
		exception.ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdBizChatParameter,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (te *ErrorChatParameter) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", te.FileName, te.LineNo, te.ErrId, te.ErrTxt)
}

func (te *ErrorChatParameter) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorChatParameter](target); ok {
		return ed.ErrId == te.ErrId
	}
	return false
}
