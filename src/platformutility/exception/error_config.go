package exception

import (
	"errors"
	"fmt"
	"runtime"
)

type ConfigErrorDescription struct {
	ErrorDescription
}

var ConfigError = &ConfigErrorDescription{
	ErrorDescription{
		ErrTxt:   "",
		ErrDesc:  ErrorIdSysConfigRWTxt,
		ErrId:    ErrorIdSysConfigRW,
		FileName: "",
		LineNo:   -1,
	},
}

func ConfigErrorEx(errTxt string) *ConfigErrorDescription {
	_, file, line, _ := runtime.Caller(1)
	return &ConfigErrorDescription{
		ErrorDescription{
			ErrTxt:   errTxt,
			ErrDesc:  errTxt,
			ErrId:    ErrorIdSysConfigRW,
			FileName: file,
			LineNo:   line,
		},
	}
}

func ConfigErrorEx2(errTxt string, a ...interface{}) *ConfigErrorDescription {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ConfigErrorDescription{
		ErrorDescription{
			ErrTxt:   argsString,
			ErrDesc:  errTxt,
			ErrId:    ErrorIdSysConfigRW,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (e *ConfigErrorDescription) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", e.FileName, e.LineNo, e.ErrId, e.ErrTxt)
}

func (e *ConfigErrorDescription) Is(target error) bool {
	if ed, ok := errors.AsType[*ConfigErrorDescription](target); ok {
		return ed.ErrId == e.ErrId
	}

	return false
}
