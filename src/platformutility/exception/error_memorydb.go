package exception

import (
	"errors"
	"fmt"
	"runtime"
)

type MemoryDbError struct {
	ErrorDescription
}

var MemoryDbErr = &MemoryDbError{
	ErrorDescription{
		ErrTxt:   "",
		ErrId:    errorIdMemoryDbError,
		FileName: "",
		LineNo:   -1,
	},
}

func MemoryDbErrEx(errTxt string) *MemoryDbError {
	_, file, line, _ := runtime.Caller(1)
	return &MemoryDbError{
		ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    errorIdMemoryDbError,
			FileName: file,
			LineNo:   line,
		},
	}
}

func MemoryDbErrEx2(errTxt string, a ...interface{}) *MemoryDbError {
	errorString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &MemoryDbError{
		ErrorDescription{
			ErrTxt:   errorString,
			ErrId:    errorIdMemoryDbError,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (e *MemoryDbError) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", e.FileName, e.LineNo, e.ErrId, e.ErrTxt)
}

func (e *MemoryDbError) Is(target error) bool {
	if ed, ok := errors.AsType[*MemoryDbError](target); ok {
		return ed.ErrId == e.ErrId
	}

	return false
}
