/**
* prompt 计算器剩余
*
* context_window预算，在service/runtime/message_builder.go中构建请求时实现：
*	BuildChatCompletionRequest/BuildChatCompletionRequestWithTool 每次请求时调用 DynamicMaxTokens
*	EstimateInputTokens:CJK 约为1.5 字符/token、其他约为 4 字符/token、每条消息 + 4 token开销、乘以 1.2的余量
*	dynamicMaxTokens：max_tokens = min(ContextWindow - 估算输入，provider单次输出上限)，保证[估算输入+ max_tokens <= ContextWindow]，避免长输入超窗口被供应商拒绝
*	ContextWindow = 0(未配置)时不发送max_tokens，由供应商用默认值
*
*	这属于输出预算侧压缩，只裁剪max_tokens，不裁剪输入本身。
**/

package ctxmgr

import (
	"my.favorite/cclawservice/model/dataobject"
	sCommon "my.favorite/cclawservice/service/common"
	pCommon "my.favorite/platformutility/common"
)

// DynamicMaxTokens 计算应发送的 max_tokens。由 context_window 与供应商输出上限共同约束:
//   - 取值为 min(context_window - 估算输入, llmProvider.MaxTokens);
//   - 剩余空间 ≤ 0 时取 1(此时请求大概率仍会被供应商拒绝,但保持确定性);
//   - 未配置 ContextWindow 时返回 0(不发送 max_tokens,由供应商用其默认值)。
//
// 修复背景:早期版本仅按 context_window 推导(如 1M 窗口 + 小输入 → max_tokens ≈ 1M),
func DynamicMaxTokens(llmProvider dataobject.TLlm, messages []sCommon.ChatMessage) int {
	if llmProvider.ContextWindow == 0 {
		return 0 // 未配置返回 0 表示应用供应商的默认值
	}

	budget := int(llmProvider.ContextWindow) - EstimateInputTokens(messages)
	if budget < 1 {
		budget = 1
	}
	if budget > llmProvider.MaxTokens {
		//确保不会超过LLM供应商单次输出的上限
		budget = llmProvider.MaxTokens
	}
	return budget
}

// EstimateInputTokens 粗略估算 messages 的输入 token 数(非精确,仅用于动态裁剪)。
// 估算规则:
//   - 中文/日文/韩文(CJK)字符按 ~1.5 字符/token;
//   - 其他字符(如英文/数字/标点)按 ~4 字符/token;
//   - 每条消息附加 ~4 token 的角色/结构开销;
//   - 结果上浮 20% 作为安全余量,避免估算偏低导致"输入 + max_tokens"仍越界。
func EstimateInputTokens(messages []sCommon.ChatMessage) int {
	const (
		charsPerCJKToken   = 1.5
		charsPerAsciiToken = 4.0
		perMessageOverhead = 4
		safetyFactor       = 0.2
	)

	total := 0
	for _, m := range messages {
		cjk, other := 0, 0
		for _, r := range m.Content {
			if pCommon.IsCJK(r) {
				cjk++
			} else {
				other++
			}
		}
		total += perMessageOverhead + int(float64(cjk)/charsPerCJKToken) + int(float64(other)/charsPerAsciiToken)
	}
	return int(float64(total) * (1 + safetyFactor))
}
