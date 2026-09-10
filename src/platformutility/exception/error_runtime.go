package exception

import (
	"errors"
	"fmt"
	"runtime"
)

type ErrorRuntime struct {
	ErrorDescription
}

func RuntimeErr(errTxt string) *ErrorRuntime {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorRuntime{
		ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdUnInitialized,
			FileName: file,
			LineNo:   line,
		},
	}
}

func RuntimeErr2(errTxt string, a ...interface{}) *ErrorRuntime {
	errorText := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorRuntime{
		ErrorDescription{
			ErrTxt:   errorText,
			ErrId:    ErrorIdUnInitialized,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (re *ErrorRuntime) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", re.FileName, re.LineNo, re.ErrId, re.ErrTxt)
}

func (re *ErrorRuntime) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorRuntime](target); ok {
		return ed.ErrId == re.ErrId
	}
	return false
}
