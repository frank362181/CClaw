package exception

import (
	"errors"
	"fmt"
	"runtime"

	"my.favorite/platformutility/exception"
)

// ErrorPrompt 定义 prompt 相关的业务错误
type ErrorPrompt struct {
	exception.ErrorDescription
}

var PromptErr = &ErrorPrompt{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizPrompt,
		FileName: "",
		LineNo:   -1,
	},
}

func PromptErrEx(errTxt string) *ErrorPrompt {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorPrompt{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizPrompt,
			FileName: file,
			LineNo:   line,
		},
	}
}

func PromptErrEx2(errTxt string, a ...interface{}) *ErrorPrompt {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)

	return &ErrorPrompt{
		exception.ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdBizPrompt,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (te *ErrorPrompt) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", te.FileName, te.LineNo, te.ErrId, te.ErrTxt)
}

func (te *ErrorPrompt) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorPrompt](target); ok {
		return ed.ErrId == te.ErrId
	}

	return false
}
