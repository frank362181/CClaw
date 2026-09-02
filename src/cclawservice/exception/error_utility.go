package exception

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
)

// LlmAPIError 供应商返回的错误结构(OpenAI 风格)
type LlmAPIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// MapStartupFileError 把启动文件相关的仓库层/平台错误转换为 agent 管理业务错误,
// 使 controller 只处理业务错误,不感知底层平台异常。
func MapStartupFileError(err error) error {
	switch {
	case errors.Is(err, exception.FilePathError):
		// 启动文件不在白名单/路径越界:视为参数错误,不暴露具体路径
		return AgentParameterErrEx("illegal startup file")
	case errors.Is(err, exception.ParameterEmptyError):
		return AgentInternalErrEx("agent service internal error")
	default:
		return err
	}
}

// LlmError 统一转换 LLM 调用错误:客户端取消 → ChatCanceledErr(静默),其余 → ChatLLMErr。
func LlmError(err error, logPrefix string) error {
	if errors.Is(err, ErrRequestCanceled) {
		logging.LogDebugF("%s: request canceled by client", logPrefix)
		return ChatCanceledErrEx("chat request canceled")
	}
	logging.LogErrorF("%s:%s", logPrefix, err.Error())
	return ChatLLMErrEx2("%s:%s", logPrefix, err.Error())
}

// ParseLlmResponseError 对LlmAPIError的返回值进行解析，便于阅读和理解
func ParseLlmResponseError(resp *http.Response, readBytes int64) error {
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, readBytes))
	var apiErr LlmAPIError
	if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Message != "" {
		return fmt.Errorf("runtime client: http %d:%s", resp.StatusCode, apiErr.Message)
	}

	return fmt.Errorf("runtime client: http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
}
