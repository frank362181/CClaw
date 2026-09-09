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

// ErrorChatToolLoop 工具调用循环超过最大轮次仍未产出最终答复(HTTP 502:任务步骤过多未完成)。
// 与 ErrorChatLLM 区分:不是引擎不可用,而是任务规模超出单回合工具循环预算,
// 应提示用户缩小范围/分批执行,而不是让其"稍后再试"。
// 模式与 ErrorChatLLM 保持一致。
type ErrorChatToolLoop struct {
	exception.ErrorDescription
}

var ChatToolLoopErr = &ErrorChatToolLoop{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizChatToolLoop,
		FileName: "",
		LineNo:   -1,
	},
}

func ChatToolLoopErrEx(errTxt string) *ErrorChatToolLoop {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatToolLoop{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizChatToolLoop,
			FileName: file,
			LineNo:   line,
		},
	}
}

func ChatToolLoopErrEx2(errTxt string, a ...interface{}) *ErrorChatToolLoop {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatToolLoop{
		exception.ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdBizChatToolLoop,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (te *ErrorChatToolLoop) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", te.FileName, te.LineNo, te.ErrId, te.ErrTxt)
}

func (te *ErrorChatToolLoop) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorChatToolLoop](target); ok {
		return ed.ErrId == te.ErrId
	}
	return false
}

// ErrorChatLLMQuota LLM 上游额度耗尽/欠费(HTTP 402/insufficient_quota/out of quota 等)。
// 与 ErrorChatLLM 区分:不是临时不可用,而是需要用户充值/提升配额,给可读提示。
// 模式与 ErrorChatLLM / ErrorChatToolLoop 保持一致。
type ErrorChatLLMQuota struct {
	exception.ErrorDescription
}

var ChatLLMQuotaErr = &ErrorChatLLMQuota{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizChatLLMQuota,
		FileName: "",
		LineNo:   -1,
	},
}

func ChatLLMQuotaErrEx(errTxt string) *ErrorChatLLMQuota {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatLLMQuota{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizChatLLMQuota,
			FileName: file,
			LineNo:   line,
		},
	}
}

func ChatLLMQuotaErrEx2(errTxt string, a ...interface{}) *ErrorChatLLMQuota {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatLLMQuota{
		exception.ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdBizChatLLMQuota,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (te *ErrorChatLLMQuota) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", te.FileName, te.LineNo, te.ErrId, te.ErrTxt)
}

func (te *ErrorChatLLMQuota) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorChatLLMQuota](target); ok {
		return ed.ErrId == te.ErrId
	}
	return false
}

// ErrorChatRunTimeout 单次聊天任务(工具循环+LLM 生成)超过应用级运行超时。
// 与传输层 writeTimeout 解耦:任务时长由应用层(chatRunTimeout)界定,
// 超时给可读的"任务超时"提示,而不是笼统的"请求已被取消/引擎不可用"。
// 模式与 ErrorChatLLM / ErrorChatToolLoop / ErrorChatLLMQuota 保持一致。
type ErrorChatRunTimeout struct {
	exception.ErrorDescription
}

var ChatRunTimeoutErr = &ErrorChatRunTimeout{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizChatRunTimeout,
		FileName: "",
		LineNo:   -1,
	},
}

func ChatRunTimeoutErrEx(errTxt string) *ErrorChatRunTimeout {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatRunTimeout{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizChatRunTimeout,
			FileName: file,
			LineNo:   line,
		},
	}
}

func ChatRunTimeoutErrEx2(errTxt string, a ...interface{}) *ErrorChatRunTimeout {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatRunTimeout{
		exception.ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdBizChatRunTimeout,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (te *ErrorChatRunTimeout) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", te.FileName, te.LineNo, te.ErrId, te.ErrTxt)
}

func (te *ErrorChatRunTimeout) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorChatRunTimeout](target); ok {
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
var ErrRequestCanceled = errors.New("runtime client: request context canceled")

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

// ErrorChatBusy 目标 Agent 已达并发上限(HTTP 503:该智能体当前繁忙,请稍后再试)。
// 模式与 ErrorChatNotActive 保持一致。
type ErrorChatBusy struct {
	exception.ErrorDescription
}

var ChatBusyErr = &ErrorChatBusy{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizChatBusy,
		FileName: "",
		LineNo:   -1,
	},
}

func ChatBusyErrEx(errTxt string) *ErrorChatBusy {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatBusy{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizChatBusy,
			FileName: file,
			LineNo:   line,
		},
	}
}

func ChatBusyErrEx2(errTxt string, a ...interface{}) *ErrorChatBusy {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatBusy{
		exception.ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdBizChatBusy,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (te *ErrorChatBusy) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", te.FileName, te.LineNo, te.ErrId, te.ErrTxt)
}

func (te *ErrorChatBusy) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorChatBusy](target); ok {
		return ed.ErrId == te.ErrId
	}
	return false
}
