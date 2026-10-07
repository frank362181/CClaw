/**
* 蒸馏"追加模式"的纯逻辑测试。
*
* 追加模式取代了原来的"每次让模型输出完整列表 + 整体重写",语义必须锁死:
*   - 旧条目**原样保留**、顺序不变(不抖动);
*   - 完全重复跳过;
*   - 同槽位(取值更新)原地替换;
*   - 逼近上限时改走合并模式(distillWriteMode),否则追加会溢出预算。
**/

package knowledge

import (
	"testing"

	hCommon "my.favorite/cclawservice/harness/common"
)

// TestApplyNewFacts 追加/去重/更新三种语义。
func TestApplyNewFacts(t *testing.T) {
	old := []string{"单位编码固定为 100001", "交付物统一放 /out 目录"}

	// 追加新事实:旧条目原样保留、顺序不变,新条目在尾部
	next, skipped, updated := applyNewFacts(old, []string{"对外报告统一使用简体中文"})
	if len(next) != 3 || next[0] != old[0] || next[1] != old[1] || next[2] != "对外报告统一使用简体中文" {
		t.Fatalf("追加结果不正确: %v", next)
	}
	if skipped != 0 || updated != 0 {
		t.Fatalf("skipped=%d updated=%d", skipped, updated)
	}

	// 完全重复 → 跳过,列表不变
	next, skipped, _ = applyNewFacts(old, []string{"单位编码固定为 100001"})
	if len(next) != 2 || skipped != 1 {
		t.Fatalf("完全重复应跳过: len=%d skipped=%d", len(next), skipped)
	}

	// 同槽位更新 → 原地替换(条数不变、位置不变)
	next, _, updated = applyNewFacts(old, []string{"单位编码固定为 100002"})
	if len(next) != 2 || next[0] != "单位编码固定为 100002" || updated != 1 {
		t.Fatalf("同槽位更新应原地替换: %v updated=%d", next, updated)
	}

	// 不得原地修改入参
	if old[0] != "单位编码固定为 100001" || old[1] != "交付物统一放 /out 目录" {
		t.Fatalf("不应修改入参: %v", old)
	}
}

// TestApplyNewFactsRespectsTotalCap 条目已达总量上限时,新事实不再追加(留给合并模式清理)。
func TestApplyNewFactsRespectsTotalCap(t *testing.T) {
	old := make([]string, 0, hCommon.DistillMaxMemoryFacts)
	for i := 0; i < hCommon.DistillMaxMemoryFacts; i++ {
		old = append(old, "已有事实占位"+string(rune('a'+i))+"内容足够长")
	}

	next, skipped, _ := applyNewFacts(old, []string{"一条全新的待追加事实"})
	if len(next) != len(old) || skipped == 0 {
		t.Fatalf("达总量上限后不应再追加: len=%d skipped=%d", len(next), skipped)
	}
}

// TestDistillWriteMode 追加/合并的模式选择:条目逼近上限时必须切到合并。
func TestDistillWriteMode(t *testing.T) {
	if distillWriteMode(nil) != DistillModeAppend {
		t.Fatalf("空列表应为追加模式")
	}

	many := make([]string, hCommon.DistillMergeTriggerFacts)
	for i := range many {
		many[i] = "一条足够长的事实内容用于触发合并阈值"
	}
	if distillWriteMode(many) != DistillModeMerge {
		t.Fatalf("条目数达阈值(%d)应为合并模式", hCommon.DistillMergeTriggerFacts)
	}
}
