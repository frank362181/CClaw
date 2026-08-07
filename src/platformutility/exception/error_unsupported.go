package exception

import (
	"errors"
	"fmt"
	"runtime"
)

type UnsupportedException struct {
	ErrorDescription
}

var UnsupportedExceptionError = &UnsupportedException{
	ErrorDescription: ErrorDescription{
		ErrTxt:   "",
		ErrId:    errorIdUnsupported,
		FileName: "",
		LineNo:   -1,
	},
}

func UnsupportedExceptionErrorEx(errTxt string) *UnsupportedException {
	_, file, line, _ := runtime.Caller(1)
	return &UnsupportedException{
		ErrorDescription: ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    errorIdUnsupported,
			FileName: file,
			LineNo:   line,
		},
	}
}

func UnsupportedExceptionErrorEx2(errTxt string, a ...interface{}) *UnsupportedException {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &UnsupportedException{
		ErrorDescription: ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    errorIdUnsupported,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (e *UnsupportedException) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", e.FileName, e.LineNo, e.ErrId, e.ErrTxt)
}

func (e *UnsupportedException) Is(target error) bool {
	if ed, ok := errors.AsType[*UnsupportedException](target); ok {
		return ed.ErrId == e.ErrId
	}
	return false
}
