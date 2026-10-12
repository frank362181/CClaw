/*
*
* @Summary:
*	定义全局使用的 异常和错误类型
*   所有面向全局的异常公共使用的异常基类、函数等
* @Date:
*	Jun 20, 2026
* @Author:
*	Frank Ng
 */

package exception

import (
	"context"
	"errors"
	"fmt"
	"runtime"
)

// IsContextCanceled 判断错误是否源于请求上下文取消,以便上层区分处理。
func IsContextCanceled(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) || ctx.Err() != nil
}

type ErrorDescription struct {
	ErrTxt   string
	ErrDesc  string // 面向用户的错误描述
	ErrId    int
	FileName string //错误发生的文件
	LineNo   int    //错误发生的行号
}

func (err *ErrorDescription) Description() string {
	return err.ErrDesc
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

var UnknownError = &ErrorDescription{
	ErrTxt:   "",
	ErrDesc:  "Unknown error",
	ErrId:    ErrorIdUnknown,
	FileName: "",
	LineNo:   -1,
}

func UnknownErrorEx(errTxt string) *ErrorDescription {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorDescription{
		ErrTxt:   errTxt,
		ErrId:    ErrorIdUnknown,
		ErrDesc:  errTxt,
		FileName: file,
		LineNo:   line,
	}
}

func UnknownErrorEx2(errTxt string, a ...interface{}) *ErrorDescription {
	_, file, line, _ := runtime.Caller(1)
	argText := fmt.Sprintf(errTxt, a...)
	return &ErrorDescription{
		ErrTxt:   argText,
		ErrId:    ErrorIdUnknown,
		ErrDesc:  errTxt,
		FileName: file,
		LineNo:   line,
	}
}
