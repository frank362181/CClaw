/**
* harness 调用侧的运行时错误(runtime client:构造请求/解析响应/空响应等)。
*
* 归属:属**内部成因**类错误 —— 在聊天链路上会被 exception.LlmError 归一化为
* ChatLLMKind(502 + 可读文案),因此不对外暴露业务码(Code=0)。
*
* 相关:上游 LLM 协议类型与解析/分类在 llmproto 包;聊天域 Kind 表见 error_chat.go。
**/

package exception

import (
	"net/http"

	"my.favorite/cclawservice/apperr"
	"my.favorite/platformutility/webbase/middleware/basic"
)

// codeRuntimeClientError harness 调用侧内部错误(域内单独占 1 个码位 → 4501)。
const codeRuntimeClientError = codeErrorRuntimeClientBase + 1 + iota

// RuntimeClientKind harness 调用上游失败(构造请求/解析响应/空响应等)。
// Code=0:内部成因错误不对外暴露业务码;聊天链路会归一化为 ChatLLMKind。
var RuntimeClientKind = apperr.Register(apperr.Kind{
	Name:       "harness.runtime_client",
	Code:       codeRuntimeClientError,
	HTTPStatus: http.StatusInternalServerError,
	Msg:        basic.ConstInternalServerErrorMsg,
})

// RuntimeClientErr 运行时客户端错误的哨兵(errors.Is 用)。
var RuntimeClientErr = RuntimeClientKind.Sentinel()

func RuntimeClientErrEx(errTxt string) *apperr.Error {
	return RuntimeClientKind.New(errTxt)
}

func RuntimeClientErrEx2(errTxt string, a ...interface{}) *apperr.Error {
	return RuntimeClientKind.Newf(errTxt, a...)
}
