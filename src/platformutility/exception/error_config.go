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
		ErrId:    errorIdConfigRW,
		FileName: "",
		LineNo:   -1,
	},
}

func ConfigErrorEx(errTxt string) *ConfigErrorDescription {
	_, file, line, _ := runtime.Caller(1)
	return &ConfigErrorDescription{
		ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    errorIdConfigRW,
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
