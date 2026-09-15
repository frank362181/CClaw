/**
* 维护触发的判定契约:压缩(收益/预算)× 蒸馏(内容/收尾/首次/节拍)
*
* 这些判定决定"要不要花一次 LLM 调用",判错不会报错,只会静默白烧调用或漏掉维护 —— 所以单测锁住。
**/

package ctxmgr

import (
	"strings"
	"testing"

	hCommon "my.favorite/cclawservice/harness/common"
)

// compactCase 造一个"恰好够触发"的装配结果,便于逐个维度地推翻。
func compactCase() AssembleResult {
	return AssembleResult{
		Trimmed:            true,
		DroppedFrom:        1,
		DroppedTo:          20,
		DroppedRecordCount: hCommon.CompactionMinRecords + 1,
	}
}

// TestNeedCompactGainDimension 收益维度:被裁区间体积不够时不起压缩(压完不会更小,只白烧调用)。
func TestNeedCompactGainDimension(t *testing.T) {
	result := compactCase()

	if !NeedCompact(result, hCommon.MinCompactionSrcRunes) {
		t.Fatalf("体积刚到门槛应触发: %+v", result)
	}
	if NeedCompact(result, hCommon.MinCompactionSrcRunes-1) {
		t.Error("体积不足门槛时不应触发压缩(摘要下限会把收益吃掉)")
	}
	if NeedCompact(result, 0) {
		t.Error("体积为 0(没有正文字长)时不应触发压缩")
	}
}

// TestNeedCompactBudgetDimension 预算维度:摘要已超上限时先整理摘要,不再往上加。
// 否则 system 单调增长 → 历史预算更小 → 裁更多 → 再压,正反馈。
func TestNeedCompactBudgetDimension(t *testing.T) {
	result := compactCase()
	result.Summary = strings.Repeat("摘", hCommon.MaxSummaryTotalRunes+1)

	if NeedCompact(result, hCommon.MinCompactionSrcRunes*10) {
		t.Error("摘要超总量上限时不应再压缩(应先合并摘要,否则形成正反馈)")
	}
	if !NeedMerge(result) {
		t.Error("同一状态应当判定为需要合并摘要")
	}

	// 体积再大也一样:预算维度优先
	if NeedCompact(result, 1<<20) {
		t.Error("摘要超预算时体积多大都不压")
	}
}

// TestNeedCompactKeepsCorrectnessSpine 正确性主干不得被新维度放松:
// 没有记录掉出窗口(或条数不足/预算耗尽)时永不触发。
func TestNeedCompactKeepsCorrectnessSpine(t *testing.T) {
	big := hCommon.MinCompactionSrcRunes * 10

	noDrop := compactCase()
	noDrop.DroppedFrom, noDrop.DroppedTo, noDrop.Trimmed = 0, 0, false
	if NeedCompact(noDrop, big) {
		t.Error("没有记录掉出窗口时不应压缩 —— 那会用摘要替换还能回放的原文")
	}

	short := compactCase()
	short.DroppedRecordCount = hCommon.CompactionMinRecords
	if NeedCompact(short, big) {
		t.Error("被裁条数未超过门槛时不应压缩")
	}

	exhausted := compactCase()
	exhausted.BudgetExhausted = true
	if NeedCompact(exhausted, big) {
		t.Error("历史预算为 0 时不应压缩(压缩改变不了 system 预留,问题在配置)")
	}
}

// TestNeedDistillCadenceOnly 内容稀疏时退化为节拍:第 10/20/30 轮各蒸一次。
func TestNeedDistillCadenceOnly(t *testing.T) {
	cases := map[int]bool{
		0: false, 1: false, 5: false, 6: true, // 6 轮是"首次"触发点(此前被 10 的倍数吃掉)
		9: false, 10: true, 11: false, 20: true, 30: true, 100: true,
	}

	for turns, want := range cases {
		if got := NeedDistill(turns, DistillPressure{}); got != want {
			t.Errorf("needDistill(%d, 无压力) = %v, want %v", turns, got, want)
		}
	}
}

// TestNeedDistillContentDimension 内容维度:水位之后攒够一轮就提前蒸(不必等节拍)。
// 这条同时涵盖"上一轮没排空":积压攒到一个批次自然会再次触发,不需要额外的会话状态。
func TestNeedDistillContentDimension(t *testing.T) {
	full := DistillPressure{PendingRecords: 20, PendingChars: hCommon.DistillMaxInputChars}

	// 不落在检查节拍上(1、2 轮)也不蒸 —— 内容触发按固定短间隔生效,
	// 避免蒸馏持续失败时每轮重试同一份输入
	if NeedDistill(1, full) || NeedDistill(2, full) {
		t.Error("内容够一轮但未到检查节拍时不应蒸馏")
	}
	if !NeedDistill(3, full) {
		t.Error("内容够一轮且落在检查节拍上时应蒸馏")
	}
	// 关键:内容触发不必等到 10 的倍数(第 9 轮就排空,而不是被节拍拖到第 10 轮)
	if !NeedDistill(9, full) {
		t.Error("内容触发不应只认 10 的倍数")
	}

	// 内容不足一个批次:交给节拍
	light := DistillPressure{PendingRecords: 2, PendingChars: hCommon.DistillMaxInputChars - 1}
	if NeedDistill(3, light) || NeedDistill(7, light) {
		t.Error("内容不足一个批次时不应提前蒸馏")
	}
	if !NeedDistill(10, light) {
		t.Error("内容不足时仍应走节拍兜底")
	}
}

// TestNeedDistillPressureOnlyMattersOnCheckTick 契约:压力只在检查节拍上影响判定。
// 调用方据此决定"要不要去读 MEMORY.md 算压力"—— 判定说不需要时,传什么压力结果都一样。
func TestNeedDistillPressureOnlyMattersOnCheckTick(t *testing.T) {
	full := DistillPressure{PendingRecords: 99, PendingChars: hCommon.DistillMaxInputChars * 3}

	for turns := 1; turns <= 31; turns++ {
		if NeedDistillPressure(turns) {
			if turns%hCommon.DistillCheckIntervalTurns != 0 {
				t.Fatalf("第 %d 轮不应报告\"需要压力\"", turns)
			}
			continue
		}
		if NeedDistill(turns, DistillPressure{}) != NeedDistill(turns, full) {
			t.Errorf("第 %d 轮不消费压力,但判定结果随压力变化了", turns)
		}
	}

	if NeedDistillPressure(0) {
		t.Error("未落库(序号 0)时不应要求计算压力")
	}
	if !NeedDistillPressure(hCommon.DistillCheckIntervalTurns) {
		t.Error("检查节拍上应要求计算压力")
	}
	// 收尾也不消费压力(直接判真),所以不必为它算
	if NeedDistillPressure(22) {
		t.Error("非检查节拍(收尾场景)不应要求计算压力")
	}
}

// TestNeedDistillClosingDimension 收尾维度:会话收尾必须把尾巴带走,不受节拍约束。
func TestNeedDistillClosingDimension(t *testing.T) {
	closing := DistillPressure{Closing: true}

	for _, turns := range []int{1, 6, 8, 9, 11, 22} {
		if !NeedDistill(turns, closing) {
			t.Errorf("收尾时(第 %d 轮)必须蒸馏尾巴", turns)
		}
	}

	// 本轮没有落库(序号未推进)时连收尾也不触发:没有"轮次"可言,交给其它维护动作
	if NeedDistill(0, closing) {
		t.Error("轮次序号为 0 时不应蒸馏")
	}
}
