/**
* FunctionBundle.PromptItems 的单元测试
*
* 目标是锁死那条"硬约束":system prompt 里的工具清单必须由**本轮实际下发的工具 schema** 派生 ——
* 名字集合与 bundle.Tools(= request.tools)完全一致,归属技能来自同一批 Slot.ToolNames。
* 一旦有人改成"另取一份配置渲染",这个测试就会失败。
**/

package tool

import (
	"testing"

	"my.favorite/cclawservice/harness/api/llmproto"
	hCommon "my.favorite/cclawservice/harness/common"
)

func llmTool(name, desc string) llmproto.Tool {
	return llmproto.Tool{
		Type:     "function",
		Function: llmproto.ToolFunction{Name: name, Description: desc},
	}
}

// TestFunctionBundlePromptItems 验证清单与 schema 同源、归属技能正确、空名工具被跳过。
func TestFunctionBundlePromptItems(t *testing.T) {
	bundle := FunctionBundle{
		Tools: []llmproto.Tool{
			llmTool("npa_ingest", "解析材料为按页内容 JSON"),
			llmTool("get_weather", "查实时天气"),
			llmTool("read", "读取本地文件"),
			llmTool("   ", "无名工具"),
		},
		Slots: []SkillSlot{
			{SkillId: "npa-report", ToolNames: []string{"npa_ingest"}},
			{SkillId: "weather", ToolNames: []string{"get_weather"}},
		},
	}

	items := bundle.PromptItems()
	if len(items) != len(bundle.Tools)-1 { // 空名那一条必须被跳过
		t.Fatalf("清单条数与下发工具不一致: got=%d want=%d (%+v)", len(items), len(bundle.Tools)-1, items)
	}

	want := map[string]string{
		"npa_ingest":  "npa-report",
		"get_weather": "weather",
		"read":        "", // 无技能声明 → 系统工具
	}
	for _, it := range items {
		skillId, ok := want[it.Name]
		if !ok {
			t.Fatalf("清单出现了未下发的工具: %s", it.Name)
		}
		if it.SkillId != skillId {
			t.Errorf("工具 %s 归属技能 = %q, want %q", it.Name, it.SkillId, skillId)
		}
		delete(want, it.Name)

		if it.Name == "read" {
			if it.Source != hCommon.TSourceSystem || it.OwnerTag() != "系统工具" {
				t.Errorf("无技能声明的工具应回落为系统工具: %+v", it)
			}
			continue
		}
		if it.Source != hCommon.TSourceSkill || it.OwnerTag() != "技能 "+skillId {
			t.Errorf("技能工具 %s 的归属标签不正确: %+v", it.Name, it)
		}
	}
	if len(want) != 0 {
		t.Fatalf("清单漏掉了已下发的工具: %v", want)
	}
}

// TestFunctionBundlePromptItemsEmpty 无工具时不产出清单(调用方据此省略该 section)。
func TestFunctionBundlePromptItemsEmpty(t *testing.T) {
	if items := (FunctionBundle{}).PromptItems(); len(items) != 0 {
		t.Fatalf("无工具时不应产出清单: %+v", items)
	}
}
