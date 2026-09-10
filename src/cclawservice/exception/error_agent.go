/*
* agent 管理(LLM Provider/Agent/Skill)的业务错误定义
*   struct 嵌 exception.ErrorDescription + 哨兵 + Ex/Ex2 工厂 + Error()/Is()
**/

package exception

import (
	"errors"
	"fmt"
	"runtime"

	"my.favorite/cclawservice/common"
	"my.favorite/platformutility/exception"
)

// ErrorAgentWorkspace 智能体工作空间不可用(目录缺失或 agent_dir 非法)
// 与 ErrorAgentNotFound 区分,便于前端给准确提示
// 模式与 ErrorAgentParameter / ErrorAgentNotFound 保持一致

type ErrorAgentWorkspace struct {
	exception.ErrorDescription
}

var AgentWorkspaceErr = &ErrorAgentWorkspace{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrDesc:  common.ConstErrorAgentWorkspaceMsg,
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
			ErrDesc:  common.ConstErrorAgentWorkspaceMsg,
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
			ErrDesc:  common.ConstErrorAgentWorkspaceMsg,
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
		ErrDesc:  common.ConstErrorChatLLMMsg,
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
			ErrDesc:  common.ConstErrorChatLLMMsg,
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
			ErrDesc:  common.ConstErrorChatLLMMsg,
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
