package exception

import (
	"errors"
	"fmt"
	"runtime"

	"my.favorite/platformutility/exception"
)

type ErrorDatabase struct {
	exception.ErrorDescription
}

var DatabaseQueryErr = &ErrorDatabase{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizDatabase,
		FileName: "",
		LineNo:   -1,
	},
}

func DatabaseQueryErrEx(errTxt string) *ErrorDatabase {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorDatabase{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizDatabase,
			FileName: file,
			LineNo:   line,
		},
	}
}

func DatabaseQueryErrEx2(errTxt string, a ...interface{}) *ErrorDatabase {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)

	return &ErrorDatabase{
		exception.ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdBizDatabase,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (te *ErrorDatabase) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", te.FileName, te.LineNo, te.ErrId, te.ErrTxt)
}

func (te *ErrorDatabase) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorDatabase](target); ok {
		return ed.ErrId == te.ErrId
	}

	return false
}
