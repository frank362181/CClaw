/**
* 定义所有与harness有关的异常类
**/

package exception

import (
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"

	"my.favorite/platformutility/exception"
)

type ErrorRuntimeClient struct {
	exception.ErrorDescription
}

var RuntimeClientErr = &ErrorRuntimeClient{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizHarness,
		FileName: "",
		LineNo:   -1,
	},
}

func RuntimeClientErrEx(errTxt string) *ErrorRuntimeClient {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorRuntimeClient{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizHarness,
			FileName: file,
			LineNo:   line,
		},
	}
}

func RuntimeClientErrEx2(errTxt string, a ...interface{}) *ErrorRuntimeClient {
	errorText := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorRuntimeClient{
		exception.ErrorDescription{
			ErrTxt:   errorText,
			ErrId:    ErrorIdBizHarness,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (re *ErrorRuntimeClient) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", re.FileName, re.LineNo, re.ErrId, re.ErrTxt)
}

func (re *ErrorRuntimeClient) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorRuntimeClient](target); ok {
		return ed.ErrId == re.ErrId
	}
	return false
}

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

// ErrorChatLLMAuth LLM 上游鉴权失败(API Key 无效或未配置)。
// 属"可自行修正的配置类错误",与瞬时故障区分:提示检查模型密钥配置。
// 模式与 ErrorChatBusy 保持一致。
type ErrorChatLLMAuth struct {
	exception.ErrorDescription
}

var ChatLLMAuthErr = &ErrorChatLLMAuth{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizChatLLMAuth,
		FileName: "",
		LineNo:   -1,
	},
}

func ChatLLMAuthErrEx(errTxt string) *ErrorChatLLMAuth {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatLLMAuth{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizChatLLMAuth,
			FileName: file,
			LineNo:   line,
		},
	}
}

func ChatLLMAuthErrEx2(errTxt string, a ...interface{}) *ErrorChatLLMAuth {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatLLMAuth{
		exception.ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdBizChatLLMAuth,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (te *ErrorChatLLMAuth) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", te.FileName, te.LineNo, te.ErrId, te.ErrTxt)
}

func (te *ErrorChatLLMAuth) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorChatLLMAuth](target); ok {
		return ed.ErrId == te.ErrId
	}
	return false
}

// ErrorChatLLMForbidden 无权访问模型(未开通/未授权/已下线)。属配置类错误。
// 模式与 ErrorChatLLMAuth 保持一致。
type ErrorChatLLMForbidden struct {
	exception.ErrorDescription
}

var ChatLLMForbiddenErr = &ErrorChatLLMForbidden{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizChatLLMForbidden,
		FileName: "",
		LineNo:   -1,
	},
}

func ChatLLMForbiddenErrEx(errTxt string) *ErrorChatLLMForbidden {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatLLMForbidden{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizChatLLMForbidden,
			FileName: file,
			LineNo:   line,
		},
	}
}

func ChatLLMForbiddenErrEx2(errTxt string, a ...interface{}) *ErrorChatLLMForbidden {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatLLMForbidden{
		exception.ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdBizChatLLMForbidden,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (te *ErrorChatLLMForbidden) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", te.FileName, te.LineNo, te.ErrId, te.ErrTxt)
}

func (te *ErrorChatLLMForbidden) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorChatLLMForbidden](target); ok {
		return ed.ErrId == te.ErrId
	}
	return false
}

// ErrorChatLLMNotFound 模型不存在或名称配置错误。属配置类错误。
// 模式与 ErrorChatLLMAuth 保持一致。
type ErrorChatLLMNotFound struct {
	exception.ErrorDescription
}

var ChatLLMNotFoundErr = &ErrorChatLLMNotFound{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizChatLLMNotFound,
		FileName: "",
		LineNo:   -1,
	},
}

func ChatLLMNotFoundErrEx(errTxt string) *ErrorChatLLMNotFound {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatLLMNotFound{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizChatLLMNotFound,
			FileName: file,
			LineNo:   line,
		},
	}
}

func ChatLLMNotFoundErrEx2(errTxt string, a ...interface{}) *ErrorChatLLMNotFound {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatLLMNotFound{
		exception.ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdBizChatLLMNotFound,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (te *ErrorChatLLMNotFound) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", te.FileName, te.LineNo, te.ErrId, te.ErrTxt)
}

func (te *ErrorChatLLMNotFound) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorChatLLMNotFound](target); ok {
		return ed.ErrId == te.ErrId
	}
	return false
}

// ErrorChatLLMInvalidRequest 请求被模型服务拒绝(参数/上下文/请求体不合法)。属配置类错误。
// 模式与 ErrorChatLLMAuth 保持一致。
type ErrorChatLLMInvalidRequest struct {
	exception.ErrorDescription
}

var ChatLLMInvalidRequestErr = &ErrorChatLLMInvalidRequest{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizChatLLMInvalidRequest,
		FileName: "",
		LineNo:   -1,
	},
}

func ChatLLMInvalidRequestErrEx(errTxt string) *ErrorChatLLMInvalidRequest {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatLLMInvalidRequest{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizChatLLMInvalidRequest,
			FileName: file,
			LineNo:   line,
		},
	}
}

func ChatLLMInvalidRequestErrEx2(errTxt string, a ...interface{}) *ErrorChatLLMInvalidRequest {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatLLMInvalidRequest{
		exception.ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdBizChatLLMInvalidRequest,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (te *ErrorChatLLMInvalidRequest) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", te.FileName, te.LineNo, te.ErrId, te.ErrTxt)
}

func (te *ErrorChatLLMInvalidRequest) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorChatLLMInvalidRequest](target); ok {
		return ed.ErrId == te.ErrId
	}
	return false
}

// ErrorChatLLMRateLimit 触发上游限流(可稍后重试)。
// 模式与 ErrorChatLLMAuth 保持一致。
type ErrorChatLLMRateLimit struct {
	exception.ErrorDescription
}

var ChatLLMRateLimitErr = &ErrorChatLLMRateLimit{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizChatLLMRateLimit,
		FileName: "",
		LineNo:   -1,
	},
}

func ChatLLMRateLimitErrEx(errTxt string) *ErrorChatLLMRateLimit {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatLLMRateLimit{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizChatLLMRateLimit,
			FileName: file,
			LineNo:   line,
		},
	}
}

func ChatLLMRateLimitErrEx2(errTxt string, a ...interface{}) *ErrorChatLLMRateLimit {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatLLMRateLimit{
		exception.ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdBizChatLLMRateLimit,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (te *ErrorChatLLMRateLimit) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", te.FileName, te.LineNo, te.ErrId, te.ErrTxt)
}

func (te *ErrorChatLLMRateLimit) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorChatLLMRateLimit](target); ok {
		return ed.ErrId == te.ErrId
	}
	return false
}

// ErrorChatLLMContentFilter 输入/输出被模型内容安全策略拦截(需修改输入)。
// 模式与 ErrorChatLLMAuth 保持一致。
type ErrorChatLLMContentFilter struct {
	exception.ErrorDescription
}

var ChatLLMContentFilterErr = &ErrorChatLLMContentFilter{
	exception.ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdBizChatLLMContentFilter,
		FileName: "",
		LineNo:   -1,
	},
}

func ChatLLMContentFilterErrEx(errTxt string) *ErrorChatLLMContentFilter {
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatLLMContentFilter{
		exception.ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdBizChatLLMContentFilter,
			FileName: file,
			LineNo:   line,
		},
	}
}

func ChatLLMContentFilterErrEx2(errTxt string, a ...interface{}) *ErrorChatLLMContentFilter {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &ErrorChatLLMContentFilter{
		exception.ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdBizChatLLMContentFilter,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (te *ErrorChatLLMContentFilter) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", te.FileName, te.LineNo, te.ErrId, te.ErrTxt)
}

func (te *ErrorChatLLMContentFilter) Is(target error) bool {
	if ed, ok := errors.AsType[*ErrorChatLLMContentFilter](target); ok {
		return ed.ErrId == te.ErrId
	}
	return false
}

// LlmAPIError 供应商返回的错误结构(OpenAI 风格)。
// Param 指出出错的具体字段(OpenAI 风格),便于定位请求配置问题。
type LlmAPIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param"`
	Code    string `json:"code"`
}

// LlmStatusError 结构化的 LLM 上游错误。
// 统一携带 HTTP 状态码、上游 request_id 与语义化类别,是 ParseLlmResponseError 与流中错误的
// 返回类型,便于上层按类别映射用户文案与 HTTP 状态。
type LlmStatusError struct {
	exception.ErrorDescription

	StatusCode int              // 上游 HTTP 状态码;流中错误(连接建立时已 200)记 200
	RequestID  string           // 上游 request_id(响应头),便于排障/提交工单
	Stage      string           // 错误发生阶段:response(首字节前)/ stream(流中)
	Category   LlmErrorCategory // 语义化类别
	API        LlmAPIError      // 解析出的错误体(可能为空)
	Raw        string           // 原始响应体(无法结构化解析时的兜底)
}

// LlmErrorCategory LLM 上游错误的语义化类别。
// 目的:把"可自行修正的配置类错误"(鉴权/模型名/参数/欠费)与"临时性故障"(限流/服务端)
// 分开,从而给出可操作的用户提示,而不是千篇一律的"引擎暂时不可用"。
type LlmErrorCategory string

const (
	// LlmCategoryInvalidRequest 请求非法(参数/上下文/请求体格式)
	LlmCategoryInvalidRequest LlmErrorCategory = "invalid_request"
	// LlmCategoryAuth 鉴权失败(API Key 缺失/无效)
	LlmCategoryAuth LlmErrorCategory = "auth"
	// LlmCategoryForbidden 无权限(未开通/未授权/模型下线)
	LlmCategoryForbidden LlmErrorCategory = "forbidden"
	// LlmCategoryNotFound 模型不存在或名称配置错误
	LlmCategoryNotFound LlmErrorCategory = "not_found"
	// LlmCategoryQuota 额度耗尽/欠费/账单逾期
	LlmCategoryQuota LlmErrorCategory = "quota"
	// LlmCategoryRateLimit 触发限流(可稍后重试)
	LlmCategoryRateLimit LlmErrorCategory = "rate_limit"
	// LlmCategoryServer 上游服务端故障(5xx/过载,可稍后重试)
	LlmCategoryServer LlmErrorCategory = "server"
	// LlmCategoryContentFilter 输入/输出被内容安全策略拦截
	LlmCategoryContentFilter LlmErrorCategory = "content_filter"
	// LlmCategoryUnknown 无法归类
	LlmCategoryUnknown LlmErrorCategory = "unknown"
)

const (
	llmStageResponse = "response" // 首字节前:HTTP 非 2xx 或响应体携带 error
	llmStageStream   = "stream"   // 流中:SSE 已建连(HTTP 200)后发生错误
)

// NewLlmResponseError 构造"HTTP 200 但响应体携带 error 字段"的结构化错误(同步路径)。
func NewLlmResponseError(api LlmAPIError) *LlmStatusError {
	return &LlmStatusError{
		ErrorDescription: exception.ErrorDescription{
			ErrId: ErrorIdBizLlmStatus,
		},
		StatusCode: http.StatusOK,
		Stage:      llmStageResponse,
		Category:   ClassifyLlmError(http.StatusOK, api),
		API:        api,
	}
}

// NewLlmResponseTransportError 构造同步路径(首字节前)的传输层错误(连接失败/读响应失败)。
// 归类为 server(瞬时故障),可被上层重试。
func NewLlmResponseTransportError(err error) *LlmStatusError {
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	return &LlmStatusError{
		ErrorDescription: exception.ErrorDescription{
			ErrId: ErrorIdBizLlmStatus,
		},
		Stage:    llmStageResponse,
		Category: LlmCategoryServer,
		API:      LlmAPIError{Message: detail, Type: "transport_error"},
		Raw:      detail,
	}
}

// NewLlmStreamError 构造流中错误(连接已建立,HTTP 200)的结构化错误。
func NewLlmStreamError(api LlmAPIError, requestID string) *LlmStatusError {
	return &LlmStatusError{
		ErrorDescription: exception.ErrorDescription{
			ErrId: ErrorIdBizLlmStatus,
		},
		StatusCode: http.StatusOK,
		RequestID:  requestID,
		Stage:      llmStageStream,
		Category:   ClassifyLlmError(http.StatusOK, api),
		API:        api,
	}
}

// NewLlmStreamTransportError 构造流中传输层错误(断流/读流失败/空闲超时)。
// 归类为 server(瞬时故障);携带上游 request_id 便于排障。
// 是否重试由调用方按"是否已向客户端输出内容"决定(见 IsRetryableLlmError)。
func NewLlmStreamTransportError(err error, requestID string) *LlmStatusError {
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	return &LlmStatusError{
		ErrorDescription: exception.ErrorDescription{
			ErrId: ErrorIdBizLlmStatus,
		},
		RequestID: requestID,
		Stage:     llmStageStream,
		Category:  LlmCategoryServer,
		API:       LlmAPIError{Message: detail, Type: "transport_error"},
		Raw:       detail,
	}
}

// Error 实现 error 接口;文本包含阶段/状态码/类别/上游消息/request_id,便于日志诊断。
func (e *LlmStatusError) Error() string {
	msg := strings.TrimSpace(e.API.Message)
	if msg == "" {
		msg = strings.TrimSpace(e.Raw)
	}
	var b strings.Builder
	b.WriteString("runtime client: ")
	switch {
	case e.Stage == llmStageStream:
		b.WriteString("stream error")
	case e.StatusCode > 0:
		_, _ = fmt.Fprintf(&b, "http %d", e.StatusCode)
	default:
		b.WriteString("transport error")
	}
	if e.Category != "" && e.Category != LlmCategoryUnknown {
		_, _ = fmt.Fprintf(&b, " (%s)", e.Category)
	}
	if msg != "" {
		b.WriteString(": ")
		b.WriteString(msg)
	}
	if e.RequestID != "" {
		_, _ = fmt.Fprintf(&b, " [request_id=%s]", e.RequestID)
	}
	return b.String()
}

func (e *LlmStatusError) Is(target error) bool {
	if ed, ok := errors.AsType[*LlmStatusError](target); ok {
		return ed.ErrId == e.ErrId
	}
	return false
}
