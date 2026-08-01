/*
*
* @Summary:
*	空参数的error类,因为常用,独立定义
* @Date:
*	Jun 20, 2026
* @Author:
*	Frank Ng
 */

package exception

import (
	"errors"
	"fmt"
	"runtime"
)

type ParameterEmptyErrorDescription struct {
	ErrorDescription
}

/*parameter empty error*/

var ParameterEmptyError = &ParameterEmptyErrorDescription{
	ErrorDescription: ErrorDescription{
		ErrTxt:   "",
		ErrId:    errorIdParamEmpty,
		FileName: "",
		LineNo:   -1,
	},
}

func ParameterEmptyErrorEx(errTxt string) *ParameterEmptyErrorDescription {
	_, file, line, _ := runtime.Caller(1)
	return &ParameterEmptyErrorDescription{
		ErrorDescription: ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    errorIdParamEmpty,
			FileName: file,
			LineNo:   line,
		},
	}
}

func ParameterEmptyErrorEx2(errTxt string, args ...interface{}) *ParameterEmptyErrorDescription {
	errorString := fmt.Sprintf(errTxt, args...)
	_, file, line, _ := runtime.Caller(1)
	return &ParameterEmptyErrorDescription{
		ErrorDescription: ErrorDescription{
			ErrTxt:   errorString,
			ErrId:    errorIdParamEmpty,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (e *ParameterEmptyErrorDescription) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", e.FileName, e.LineNo, e.ErrId, e.ErrTxt)
}

func (e *ParameterEmptyErrorDescription) Is(target error) bool {
	if ed, ok := errors.AsType[*ParameterEmptyErrorDescription](target); ok {
		return ed.ErrId == e.ErrId
	}

	return false
}
