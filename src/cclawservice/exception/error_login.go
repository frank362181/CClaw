package exception

import (
	"errors"
	"fmt"
	"runtime"

	"my.favorite/platformutility/exception"
)

type ErrorUserPasswordUnmatch struct {
	exception.ErrorDescription
}

var UserPasswordUnmatchErr = &ErrorUserPasswordUnmatch{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizUserNamePasswordUnmatched,
		FileName: "",
		LineNo:   -1,
	},
}

func UserPasswordUnmatchEx(errTxt string) *ErrorUserPasswordUnmatch {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorUserPasswordUnmatch{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizUserNamePasswordUnmatched,
			FileName: file,
			LineNo:   line,
		},
	}
}

func UserPasswordUnmatchEx2(errTxt string, a ...interface{}) *ErrorUserPasswordUnmatch {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorUserPasswordUnmatch{
		exception.ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdBizUserNamePasswordUnmatched,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (te *ErrorUserPasswordUnmatch) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", te.FileName, te.LineNo, te.ErrId, te.ErrTxt)
}

func (te *ErrorUserPasswordUnmatch) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorUserPasswordUnmatch](target); ok {
		return ed.ErrId == te.ErrId
	}

	return false
}

// ErrorInvalidToken ================================== about token
type ErrorInvalidToken struct {
	exception.ErrorDescription
}

var TokenInvalidErr = &ErrorInvalidToken{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizInvalidToken,
		FileName: "",
		LineNo:   -1,
	},
}

func TokenInvalidErrEx(errTxt string) *ErrorInvalidToken {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorInvalidToken{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizInvalidToken,
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
			ErrId:    ErrorIdBizInvalidToken,
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
