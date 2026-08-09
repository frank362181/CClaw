package exception

import (
	"errors"
	"fmt"
	"runtime"
)

type ErrorWebServer struct {
	ErrorDescription
}

var WebServerErr = &ErrorWebServer{
	ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdSysWebServer,
		FileName: "",
		LineNo:   -1,
	},
}

func WebServerErrEx(errTxt string) *ErrorWebServer {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorWebServer{
		ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdSysWebServer,
			FileName: file,
			LineNo:   line,
		},
	}
}

func WebServerErrEx2(errTxt string, a ...interface{}) *ErrorWebServer {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorWebServer{
		ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdSysWebServer,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (e *ErrorWebServer) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", e.FileName, e.LineNo, e.ErrId, e.ErrTxt)
}

func (e *ErrorWebServer) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorWebServer](target); ok {
		return ed.ErrId == e.ErrId
	}

	return false
}
