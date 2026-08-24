/*
* agent 管理(LLM Provider/Agent/Skill)的业务错误定义
* 模式与 error_login.go / error_prompt.go 保持一致:
*   struct 嵌 exception.ErrorDescription + 哨兵 + Ex/Ex2 工厂 + Error()/Is()
**/

package exception

import (
	"errors"
	"fmt"
	"runtime"

	"my.favorite/platformutility/exception"
)

// ErrorAgentParameter 参数错误(HTTP 400)
type ErrorAgentParameter struct {
	exception.ErrorDescription
}

var AgentParameterErr = &ErrorAgentParameter{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizAgentParameter,
		FileName: "",
		LineNo:   -1,
	},
}

func AgentParameterErrEx(errTxt string) *ErrorAgentParameter {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorAgentParameter{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizAgentParameter,
			FileName: file,
			LineNo:   line,
		},
	}
}

func AgentParameterErrEx2(errTxt string, a ...interface{}) *ErrorAgentParameter {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorAgentParameter{
		exception.ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdBizAgentParameter,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (te *ErrorAgentParameter) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", te.FileName, te.LineNo, te.ErrId, te.ErrTxt)
}

func (te *ErrorAgentParameter) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorAgentParameter](target); ok {
		return ed.ErrId == te.ErrId
	}
	return false
}

// ErrorAgentNotFound 记录不存在(HTTP 404)
type ErrorAgentNotFound struct {
	exception.ErrorDescription
}

var AgentNotFoundErr = &ErrorAgentNotFound{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizAgentNotFound,
		FileName: "",
		LineNo:   -1,
	},
}

func AgentNotFoundErrEx(errTxt string) *ErrorAgentNotFound {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorAgentNotFound{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizAgentNotFound,
			FileName: file,
			LineNo:   line,
		},
	}
}

func AgentNotFoundErrEx2(errTxt string, a ...interface{}) *ErrorAgentNotFound {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorAgentNotFound{
		exception.ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdBizAgentNotFound,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (te *ErrorAgentNotFound) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", te.FileName, te.LineNo, te.ErrId, te.ErrTxt)
}

func (te *ErrorAgentNotFound) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorAgentNotFound](target); ok {
		return ed.ErrId == te.ErrId
	}
	return false
}

// ErrorAgentConflict 记录已存在或乐观锁版本冲突(HTTP 409)
type ErrorAgentConflict struct {
	exception.ErrorDescription
}

var AgentConflictErr = &ErrorAgentConflict{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizAgentConflict,
		FileName: "",
		LineNo:   -1,
	},
}

func AgentConflictErrEx(errTxt string) *ErrorAgentConflict {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorAgentConflict{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizAgentConflict,
			FileName: file,
			LineNo:   line,
		},
	}
}

func AgentConflictErrEx2(errTxt string, a ...interface{}) *ErrorAgentConflict {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorAgentConflict{
		exception.ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdBizAgentConflict,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (te *ErrorAgentConflict) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", te.FileName, te.LineNo, te.ErrId, te.ErrTxt)
}

func (te *ErrorAgentConflict) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorAgentConflict](target); ok {
		return ed.ErrId == te.ErrId
	}
	return false
}

// ErrorAgentWorkspace 智能体工作空间不可用(目录缺失或 agent_dir 非法)
// 与 ErrorAgentNotFound 区分,便于前端给准确提示
// 模式与 ErrorAgentParameter / ErrorAgentNotFound 保持一致

type ErrorAgentWorkspace struct {
	exception.ErrorDescription
}

var AgentWorkspaceErr = &ErrorAgentWorkspace{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizAgentWorkspace,
		FileName: "",
		LineNo:   -1,
	},
}

func AgentWorkspaceErrEx(errTxt string) *ErrorAgentWorkspace {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorAgentWorkspace{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizAgentWorkspace,
			FileName: file,
			LineNo:   line,
		},
	}
}

func AgentWorkspaceErrEx2(errTxt string, a ...interface{}) *ErrorAgentWorkspace {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorAgentWorkspace{
		exception.ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdBizAgentWorkspace,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (te *ErrorAgentWorkspace) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", te.FileName, te.LineNo, te.ErrId, te.ErrTxt)
}

func (te *ErrorAgentWorkspace) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorAgentWorkspace](target); ok {
		return ed.ErrId == te.ErrId
	}
	return false
}

// ErrorAgentInternal 智能体服务内部错误(依赖未初始化等),映射为 HTTP 500
// 模式与 ErrorAgentParameter / ErrorAgentNotFound 保持一致

type ErrorAgentInternal struct {
	exception.ErrorDescription
}

var AgentInternalErr = &ErrorAgentInternal{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizAgentInternal,
		FileName: "",
		LineNo:   -1,
	},
}

func AgentInternalErrEx(errTxt string) *ErrorAgentInternal {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorAgentInternal{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizAgentInternal,
			FileName: file,
			LineNo:   line,
		},
	}
}

func AgentInternalErrEx2(errTxt string, a ...interface{}) *ErrorAgentInternal {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorAgentInternal{
		exception.ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdBizAgentInternal,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (te *ErrorAgentInternal) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", te.FileName, te.LineNo, te.ErrId, te.ErrTxt)
}

func (te *ErrorAgentInternal) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorAgentInternal](target); ok {
		return ed.ErrId == te.ErrId
	}
	return false
}
