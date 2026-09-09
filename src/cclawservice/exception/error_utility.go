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

// quotaSignals LLM 上游"额度耗尽/欠费"的特征(统一转小写匹配;含 HTTP 状态码与常见 code/文案)。
// 命中即视为用户侧额度问题,提示充值/提升配额,而不是临时性的"引擎不可用"。
var quotaSignals = []string{
	"402",                  // HTTP 402 Payment Required(如 deepseek 余额不足)
	"payment required",     // OpenAI 风格描述
	"insufficient_quota",   // OpenAI 风格 error.code
	"insufficient balance", // deepseek 余额不足
	"out of quota",         // dashscope: Throttling.User.OutOfQuota
	"quota exceeded",       // 通用配额用尽
	"billing",              // 计费相关
	"欠费", "余额不足", "额度不足", "额度耗尽", "无可用额度", "请充值",
}

// isQuotaError 判定错误是否为"LLM 额度耗尽/欠费"。
func isQuotaError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, s := range quotaSignals {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

// LlmError 统一转换 LLM 调用错误:客户端取消 → ChatCanceledErr(静默),其余 → ChatLLMErr。
// 已是带语义的聊天业务错误(如"工具循环超限" ChatToolLoopErr/"额度耗尽" ChatLLMQuotaErr)时原样透传;
// 裸错误若命中额度特征则转 ChatLLMQuotaErr,给用户可读提示而非笼统的"引擎不可用"。
func LlmError(err error, logPrefix string) error {
	if errors.Is(err, ErrRequestCanceled) {
		logging.LogDebugF("%s: request canceled by client", logPrefix)
		return ChatCanceledErrEx("chat request canceled")
	}
	logging.LogErrorF("%s:%s", logPrefix, err.Error())
	if _, ok := errors.AsType[*ErrorChatToolLoop](err); ok {
		return err
	}
	if _, ok := errors.AsType[*ErrorChatLLMQuota](err); ok {
		return err
	}
	if _, ok := errors.AsType[*ErrorChatRunTimeout](err); ok {
		return err
	}
	if isQuotaError(err) {
		return ChatLLMQuotaErrEx2("%s:%s", logPrefix, err.Error())
	}
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
