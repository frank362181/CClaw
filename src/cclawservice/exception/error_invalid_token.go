package exception

import (
	"errors"
	"fmt"
	"runtime"

	"my.favorite/cclawservice/common"
	"my.favorite/platformutility/exception"
)

type ErrorInvalidToken struct {
	exception.ErrorDescription
}

var TokenInvalidErr = &ErrorInvalidToken{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    common.ConstErrorInvalidToken,
		FileName: "",
		LineNo:   -1,
	},
}

func TokenInvalidErrEx(errTxt string) *ErrorInvalidToken {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorInvalidToken{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    common.ConstErrorInvalidToken,
			FileName: file,
			LineNo:   line,
		},
	}
}

func TokenInvalidErrEx2(errTxt string, a ...interface{}) *ErrorInvalidToken {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorInvalidToken{
		exception.ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    common.ConstErrorInvalidToken,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (te *ErrorInvalidToken) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", te.FileName, te.LineNo, te.ErrId, te.ErrTxt)
}

func (te *ErrorInvalidToken) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorInvalidToken](target); ok {
		return ed.ErrId == te.ErrId
	}

	return false
}
