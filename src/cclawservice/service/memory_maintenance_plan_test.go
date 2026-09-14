/**
* 后台记忆维护的编排契约
*
* 顺序错了不会报错,只会静默地让蒸馏看不到已被压缩的原文 —— 所以把顺序写成可测的契约。
**/

package service

import (
	"testing"
)

// TestMaintenancePlanOrder 蒸馏必须在压缩之前,合并放最后。
func TestMaintenancePlanOrder(t *testing.T) {
	cases := []struct {
		name string
		plan maintenancePlan
		want []maintenanceStep
	}{
		{"全部触发", maintenancePlan{distill: true, compact: true, merge: true},
			[]maintenanceStep{stepDistill, stepCompact, stepMerge}},
		{"只蒸馏", maintenancePlan{distill: true}, []maintenanceStep{stepDistill}},
		{"只压缩", maintenancePlan{compact: true}, []maintenanceStep{stepCompact}},
		{"只合并", maintenancePlan{merge: true}, []maintenanceStep{stepMerge}},
		{"蒸馏+压缩", maintenancePlan{distill: true, compact: true},
			[]maintenanceStep{stepDistill, stepCompact}},
		{"无动作", maintenancePlan{}, nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.plan.steps()
			if len(got) != len(c.want) {
				t.Fatalf("steps() = %v, want %v", got, c.want)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Fatalf("steps()[%d] = %d, want %d", i, got[i], c.want[i])
				}
			}
		})
	}
}

// TestMaintenancePlanDistillBeforeCompact 契约本身:两者同时触发时,蒸馏的位次必须在前。
// (蒸馏输入是"未被摘要代表的记录",压缩会把整段记录变成摘要记录 —— 顺序反了就永久丢失。)
func TestMaintenancePlanDistillBeforeCompact(t *testing.T) {
	steps := maintenancePlan{distill: true, compact: true, merge: true}.steps()

	index := map[maintenanceStep]int{}
	for i, step := range steps {
		index[step] = i
	}
	if index[stepDistill] >= index[stepCompact] {
		t.Fatalf("蒸馏必须先于压缩: %v", steps)
	}
	if index[stepMerge] != len(steps)-1 {
		t.Fatalf("合并必须在最后(它依据压缩之后的摘要集合): %v", steps)
	}
}
