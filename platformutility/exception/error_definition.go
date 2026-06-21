/*
*
* @Summary:
*	定义全局使用的 异常和错误类型
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

type ErrorDescription struct {
	ErrTxt   string
	ErrId    int
	FileName string //错误发生的文件
	LineNo   int    //错误发生的行号
}

func (err *ErrorDescription) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", err.FileName, err.LineNo, err.ErrId, err.ErrTxt)
}

func (err *ErrorDescription) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorDescription](target); ok {
		return ed.ErrId == err.ErrId
	}

	return false
}

var FileExtensionError = &ErrorDescription{
	ErrTxt:   "",
	ErrId:    errorIdFileExtension,
	FileName: "",
	LineNo:   -1,
}

func FileExtensionErrorEx(errTxt string) *ErrorDescription {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorDescription{
		ErrTxt:   errTxt,
		ErrId:    errorIdFileExtension,
		FileName: file,
		LineNo:   line,
	}
}

var UnloadFileError = &ErrorDescription{
	ErrTxt:   "",
	ErrId:    errorIdUnloadFile,
	FileName: "",
	LineNo:   -1,
}

func UnloadFileErrorEx(errTxt string) *ErrorDescription {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorDescription{
		ErrTxt:   errTxt,
		ErrId:    errorIdUnloadFile,
		FileName: file,
		LineNo:   line,
	}
}

var UnknownError = &ErrorDescription{
	ErrTxt:   "",
	ErrId:    errorIdUnknown,
	FileName: "",
	LineNo:   -1,
}

func UnknownErrorEx(errTxt string) *ErrorDescription {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorDescription{
		ErrTxt:   errTxt,
		ErrId:    errorIdUnknown,
		FileName: file,
		LineNo:   line,
	}
}
