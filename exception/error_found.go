/*
*
* @Summary:
*	查找结果错误定义,因为常用,独立定义
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

// found error

type MatchErrorDescription struct {
	ErrorDescription
}

/*parameter empty error*/

var MatchError = &MatchErrorDescription{
	ErrorDescription: ErrorDescription{
		ErrTxt:   "",
		ErrId:    errorIdMatch,
		FileName: "",
		LineNo:   -1,
	},
}

func MatchErrorEx(errTxt string) *MatchErrorDescription {
	_, file, line, _ := runtime.Caller(1)
	return &MatchErrorDescription{
		ErrorDescription: ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    errorIdMatch,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (e *MatchErrorDescription) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", e.FileName, e.LineNo, e.ErrId, e.ErrTxt)
}

func (e *MatchErrorDescription) Is(target error) bool {
	if ed, ok := errors.AsType[*MatchErrorDescription](target); ok {
		return ed.ErrId == e.ErrId
	}

	return false
}

// unfound error

type UnMatchErrorDescription struct {
	ErrorDescription
}

var UnMatchError = &UnMatchErrorDescription{
	ErrorDescription: ErrorDescription{
		ErrTxt:   "",
		ErrId:    errorIdUnMatch,
		FileName: "",
		LineNo:   -1,
	},
}

func UnMatchErrorEx(errTxt string) *UnMatchErrorDescription {
	_, file, line, _ := runtime.Caller(1)
	return &UnMatchErrorDescription{
		ErrorDescription: ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    errorIdUnMatch,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (e *UnMatchErrorDescription) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", e.FileName, e.LineNo, e.ErrId, e.ErrTxt)
}

func (e *UnMatchErrorDescription) Is(target error) bool {
	if ed, ok := errors.AsType[*UnMatchErrorDescription](target); ok {
		return ed.ErrId == e.ErrId
	}

	return false
}
