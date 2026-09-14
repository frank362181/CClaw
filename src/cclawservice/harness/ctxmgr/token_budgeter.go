/**
* LLM token budgeter
*
*	对LLM的各个组成、context window的大小做预算
**/

package ctxmgr

import (
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/llmproto"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
)

func SummaryOverBudget(records []hCommon.MemoryRecord) bool {
	return hCommon.SummariesRunes(records) > hCommon.MaxSummaryTotalRunes
}

// BoostrapBudget 计算启动文档注入的总预算
func BoostrapBudget(configured int) int {
	if configured > 0 {
		return configured
	}

	return hCommon.MaxBootstrapTotalChars
}

func Budget(policy ContextPolicy, msg llmproto.ChatMessage) int {
	reserved := calcSystemReservedTokens(policy, msg)
	budget := policy.ContextWindow - policy.OutputReserve - reserved

	return budget
}

// DynamicMaxTokens 计算应发送的 max_tokens。由 context_window 与供应商输出上限共同约束:
//   - 取值为 min(context_window - 估算输入, llmProvider.MaxTokens);
//   - 剩余空间 ≤ 0 时取 1(此时请求大概率仍会被供应商拒绝,但保持确定性);
//   - 未配置 ContextWindow 时返回 0(不发送 max_tokens,由供应商用其默认值)。
//
// 修复背景:早期版本仅按 context_window 推导(如 1M 窗口 + 小输入 → max_tokens ≈ 1M),
func DynamicMaxTokens(llmProvider dataobject.TLlm, messages []llmproto.ChatMessage) int {
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

// token 估算的假设(公开为包内常量:它同时被“输出上限是否覆盖字符上限”的契约测试引用)。
const (
	// CharsPerCJKToken CJK 字符每 token 的字符数(即 1 token ≈ 1.5 个汉字)。
	CharsPerCJKToken = 1.5
	// CharsPerAsciiToken 非 CJK 字符每 token 的字符数。
	CharsPerAsciiToken   = 4.0
	perMessageOverhead   = 4
	estimateSafetyFactor = 0.2
)

// EstimateInputTokens 粗略估算 messages 的输入 token 数(非精确,仅用于动态裁剪)。
// 估算规则:
//   - 中文/日文/韩文(CJK)字符按 ~1.5 字符/token;
//   - 其他字符(如英文/数字/标点)按 ~4 字符/token;
//   - 每条消息附加 ~4 token 的角色/结构开销;
//   - 结果上浮 20% 作为安全余量,避免估算偏低导致"输入 + max_tokens"仍越界。
func EstimateInputTokens(messages []llmproto.ChatMessage) int {
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
		total += perMessageOverhead + int(float64(cjk)/CharsPerCJKToken) + int(float64(other)/CharsPerAsciiToken)
	}
	return int(float64(total) * (1 + estimateSafetyFactor))
}

// NeedDistillNow 判断是否蒸馏
func NeedDistillNow(turns int) bool {
	return turns >= hCommon.DistillMinTurns && turns%hCommon.DistillIntervalTurns == 0
}

// ======================== private functions & methods ===========================
// calcSystemReservedTokens 计算系统提示应占用的 token 额度。
//
// 取 max(系统提示实际估算, policy.ReverseTokenFloor):
//   - reverse_token_floor 的语义是"为 AGENTS.md 等启动文档预留的最小空间",
//     即它约束的是**历史可以占多少**,而不是"历史至少保留多少";
//   - 系统提示实际更大时以实际为准(不能因为 floor 小就超窗)。
//
// 历史摘要会被折叠进 system 消息,因此这个额度同时也保护了摘要不被历史挤掉。
func calcSystemReservedTokens(policy ContextPolicy, sysMsg llmproto.ChatMessage) int {
	reversed := EstimateInputTokens([]llmproto.ChatMessage{sysMsg})
	if policy.ReverseTokenFloor > reversed {
		return policy.ReverseTokenFloor
	}

	return reversed
}
