/*
* chat 业务错误定义
* 模式与 error_agent.go / error_prompt.go 保持一致:
*   struct 嵌 exception.ErrorDescription + 哨兵 + Ex/Ex2 工厂 + Error()/Is()
**/

package exception

import (
	"errors"
	"fmt"
	"runtime"

	"my.favorite/platformutility/exception"
)

// ErrorChatLLM LLM 引擎调用失败(HTTP 502:上游智能体引擎不可用)
type ErrorChatLLM struct {
	exception.ErrorDescription
}

var ChatLLMErr = &ErrorChatLLM{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizChatLLM,
		FileName: "",
		LineNo:   -1,
	},
}

func ChatLLMErrEx(errTxt string) *ErrorChatLLM {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatLLM{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizChatLLM,
			FileName: file,
			LineNo:   line,
		},
	}
}

func ChatLLMErrEx2(errTxt string, a ...interface{}) *ErrorChatLLM {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatLLM{
		exception.ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdBizChatLLM,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (te *ErrorChatLLM) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", te.FileName, te.LineNo, te.ErrId, te.ErrTxt)
}

func (te *ErrorChatLLM) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorChatLLM](target); ok {
		return ed.ErrId == te.ErrId
	}
	return false
}

// ErrorChatCanceled 请求上下文被取消(客户端断开/超时);HTTP 499(客户端关闭请求)或直接静默结束
// 模式与 error_agent.go 保持一致。
type ErrorChatCanceled struct {
	exception.ErrorDescription
}

var ChatCanceledErr = &ErrorChatCanceled{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizChatCanceled,
		FileName: "",
		LineNo:   -1,
	},
}

func ChatCanceledErrEx(errTxt string) *ErrorChatCanceled {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatCanceled{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizChatCanceled,
			FileName: file,
			LineNo:   line,
		},
	}
}

func ChatCanceledErrEx2(errTxt string, a ...interface{}) *ErrorChatCanceled {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatCanceled{
		exception.ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdBizChatCanceled,
			FileName: file,
			LineNo:   line,
		},
	}
}

// ErrRequestCanceled 表示请求上下文被取消(客户端断开/超时),而非上游故障。
// 上层可用 errors.Is(err, ErrRequestCanceled) 区分"客户端主动取消"与"引擎异常"。
var ErrRequestCanceled = errors.New("llm client: request context canceled")

func (te *ErrorChatCanceled) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", te.FileName, te.LineNo, te.ErrId, te.ErrTxt)
}

func (te *ErrorChatCanceled) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorChatCanceled](target); ok {
		return ed.ErrId == te.ErrId
	}
	return false
}

// ErrorChatNotActive 中断请求时,目标会话没有进行中的聊天(HTTP 400)。
// 即:聊天尚未开始或已结束,此时不应支持中断调用。
// 模式与 error_agent.go / ErrorChatCanceled 保持一致。
type ErrorChatNotActive struct {
	exception.ErrorDescription
}

var ChatNotActiveErr = &ErrorChatNotActive{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizChatNotActive,
		FileName: "",
		LineNo:   -1,
	},
}

func ChatNotActiveErrEx(errTxt string) *ErrorChatNotActive {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatNotActive{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizChatNotActive,
			FileName: file,
			LineNo:   line,
		},
	}
}

func ChatNotActiveErrEx2(errTxt string, a ...interface{}) *ErrorChatNotActive {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatNotActive{
		exception.ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdBizChatNotActive,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (te *ErrorChatNotActive) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", te.FileName, te.LineNo, te.ErrId, te.ErrTxt)
}

func (te *ErrorChatNotActive) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorChatNotActive](target); ok {
		return ed.ErrId == te.ErrId
	}
	return false
}
