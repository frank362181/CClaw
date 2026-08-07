package exception

import (
	"errors"
	"fmt"
	"runtime"

	"my.favorite/cclawservice/common"
	"my.favorite/platformutility/exception"
)

type ErrorUserPasswordUnmatch struct {
	exception.ErrorDescription
}

var UserPasswordUnmatchErr = &ErrorUserPasswordUnmatch{
	exception.ErrorDescription{
		ErrTxt:   common.ConstErrorUserNamePasswordUnmatchMsg,
		ErrId:    common.ConstErrorUserNamePasswordUnmatch,
		FileName: "",
		LineNo:   -1,
	},
}

func UserPasswordUnmatchEx(errTxt string) *ErrorUserPasswordUnmatch {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorUserPasswordUnmatch{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    common.ConstErrorUserNamePasswordUnmatch,
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
			ErrId:    common.ConstErrorUserNamePasswordUnmatch,
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
