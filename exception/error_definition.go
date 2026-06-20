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

// =================== found error =========================
type foundError struct {
	errText string
	errID   int
}

var FoundError = &foundError{errText: "found", errID: 1}

func (e *foundError) Error() string {
	_, file, line, _ := runtime.Caller(1)
	return fmt.Sprintf("found error: [%s:%d]:%d: %s", file, line, e.errID, e.errText)
}

func (e *foundError) Is(err error) bool {
	return errors.Is(err, e)
}

// =================== not found error ======================
type notFoundError struct {
	errText string //错误的文本
	errID   int    //错误编码
}

var NotFoundError = &notFoundError{
	errText: "unfound",
	errID:   2,
}

func (e *notFoundError) Error() string {
	_, file, line, _ := runtime.Caller(1)
	return fmt.Sprintf("found error: [%s:%d]:%d: %s", file, line, e.errID, e.errText)
}

func (e *notFoundError) Is(err error) bool {
	return errors.Is(err, e)
}
