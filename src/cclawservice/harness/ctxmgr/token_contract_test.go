/**
* 输出 token 上限与"代码承诺的字符上限"的对齐契约
*
* 背景:摘要/蒸馏这类维护调用,输出 token 上限(SummaryMaxTokens)必须 ≥ 代码自己承诺的
* 字符上限,否则契约与能力自相矛盾 —— 模型写满就被上游截断,而 finish_reason=length 在
* summarizer 里被当作失败:同一段记录每轮重试、永远压不掉(大段中文对话刚好落在这个区间)。
**/

package ctxmgr

import (
	"testing"

	hCommon "my.favorite/cclawservice/harness/common"
)

// TestSummaryMaxTokensCoversRuneCaps 输出上限必须装得下代码承诺的字符上限。
func TestSummaryMaxTokensCoversRuneCaps(t *testing.T) {
	// 最坏情况按 CJK 估算:1 token ≈ CharsPerCJKToken 个汉字
	capabilityRunes := float64(hCommon.SummaryMaxTokens) * CharsPerCJKToken

	if capabilityRunes < float64(hCommon.MaxSummaryRunes) {
		t.Errorf("SummaryMaxTokens(%d) 只能产出约 %.0f 个汉字,小于单条摘要的落盘上限 MaxSummaryRunes(%d):"+
			"模型会顶到 max_tokens 被截断,而截断在调用方被当作失败 → 该段永远压不掉",
			hCommon.SummaryMaxTokens, capabilityRunes, hCommon.MaxSummaryRunes)
	}

	// 蒸馏要求模型回"更新后的完整列表":上限条数 × 单条上限
	distillContractRunes := float64(hCommon.DistillMaxMemoryFacts * hCommon.DistillMaxFactRunes)
	if capabilityRunes < distillContractRunes {
		t.Errorf("SummaryMaxTokens(%d) 只能产出约 %.0f 个汉字,装不下蒸馏契约要求的"+
			"DistillMaxMemoryFacts(%d) × DistillMaxFactRunes(%d) = %.0f 个汉字;"+
			"模型回不全列表 → finish_reason=length → 整轮作废",
			hCommon.SummaryMaxTokens, capabilityRunes,
			hCommon.DistillMaxMemoryFacts, hCommon.DistillMaxFactRunes, distillContractRunes)
	}
}
