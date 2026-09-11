/**
* 上游 HTTP 层面的协议工具:request_id 提取、错误体判定、非 2xx 响应解析。
*
* 纯协议层:不引用任何业务 Kind。
**/

package llmproto

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	pCommon "my.favorite/platformutility/common"
)

// requestIDHeaders 上游 request_id 可能出现的响应头(按优先级;http.Header.Get 大小写不敏感)。
var requestIDHeaders = []string{
	"X-Request-Id",
	"X-Dashscope-Request-Id",
	"Request-Id",
	"X-Trace-Id",
	"Trace-Id",
}

// GetRequestIDFromLlmResponse 从上游响应头提取 request_id,便于排障/提交工单。
func GetRequestIDFromLlmResponse(header http.Header) string {
	if header == nil {
		return pCommon.BlankString
	}
	for _, h := range requestIDHeaders {
		if v := strings.TrimSpace(header.Get(h)); v != pCommon.BlankString {
			return v
		}
	}
	return pCommon.BlankString
}

// HasError 判定 JSON 载荷中是否携带错误体(error 字段存在且非 null)。
// 供调用方判断上游是否返回错误,而无需感知错误体的内部结构。
func HasError(payload json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(payload))
	return trimmed != pCommon.BlankString && trimmed != "null"
}

// ParseLlmResponseError 解析上游非 2xx 响应,返回带 HTTP 状态码、request_id 与语义化类别的
// 结构化错误(*LlmStatusError),供上层按类别映射用户文案。
func ParseLlmResponseError(resp *http.Response, readBytes int64) error {
	statusCode := 0
	requestID := ""
	if resp != nil {
		statusCode = resp.StatusCode
		requestID = GetRequestIDFromLlmResponse(resp.Header)
	}

	raw := ""
	if resp != nil && resp.Body != nil {
		if data, err := io.ReadAll(io.LimitReader(resp.Body, readBytes)); err == nil {
			raw = strings.TrimSpace(string(data))
		}
	}

	apiErr := parseLlmErrorBody(raw)
	return &LlmStatusError{
		StatusCode: statusCode,
		RequestID:  requestID,
		Stage:      llmStageResponse,
		Category:   ClassifyLlmError(statusCode, apiErr),
		api:        apiErr,
		raw:        raw,
	}
}
