package exception

import (
	"errors"
	"fmt"
	"runtime"
)

type DatabaseError struct {
	ErrorDescription
}

var DatabaseErr = &DatabaseError{
	ErrorDescription{
		ErrTxt:   "",
		ErrDesc:  ErrorIdSysDatabaseTxt,
		ErrId:    ErrorIdSysDatabase,
		FileName: "",
		LineNo:   -1,
	},
}

func DatabaseErrorEx(errTxt string) *DatabaseError {
	_, file, line, _ := runtime.Caller(1)
	return &DatabaseError{
		ErrorDescription{
			ErrTxt:   errTxt,
			ErrDesc:  errTxt,
			ErrId:    ErrorIdSysDatabase,
			FileName: file,
			LineNo:   line,
		},
	}
}

func DatabaseErrorEx2(errTxt string, a ...interface{}) *DatabaseError {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &DatabaseError{
		ErrorDescription{
			ErrTxt:   argsString,
			ErrDesc:  errTxt,
			ErrId:    ErrorIdSysDatabase,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (e *DatabaseError) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", e.FileName, e.LineNo, e.ErrId, e.ErrTxt)
}

func (e *DatabaseError) Is(target error) bool {
	if ed, ok := errors.AsType[*DatabaseError](target); ok {
		return ed.ErrId == e.ErrId
	}

	return false
}
