package exception

import (
	"errors"
	"fmt"
	"runtime"
)

type SqlExecuteError struct {
	ErrorDescription
}

var SqlExecuteErr = &SqlExecuteError{
	ErrorDescription{
		ErrTxt:   "",
		ErrId:    errorIdSqlError,
		FileName: "",
		LineNo:   -1,
	},
}

func SqlExecuteErrEx(errTxt string) *SqlExecuteError {
	_, file, line, _ := runtime.Caller(1)
	return &SqlExecuteError{
		ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    errorIdSqlError,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (e *SqlExecuteError) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", e.FileName, e.LineNo, e.ErrId, e.ErrTxt)
}

func (e *SqlExecuteError) Is(target error) bool {
	if ed, ok := errors.AsType[*SqlExecuteError](target); ok {
		return ed.ErrId == e.ErrId
	}

	return false
}
