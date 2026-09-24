/**
* "Available Tools" 段(本轮可调用工具清单)的渲染测试
*
* 覆盖:
*   - 空清单 / 全空名 → 不产出 section(避免空段占预算);
*   - 正常渲染:一行一个工具,含名字、归属标签与一句话用途,末尾带"事实源"提醒;
*   - 两层预算:单条描述截断(MaxToolDescPromptRunes)与总量截断(MaxToolsPromptChars,保前丢后);
*   - 段位顺序:必须排在 Skill 正文之前(尾部预算从后往前丢,工具清单比 SOP 更不该被丢)。
**/

package ctxmgr

import (
	"fmt"
	"strings"
	"testing"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
)

// TestRenderToolsCatalog 锁定工具清单的渲染口径与两层预算。
func TestRenderToolsCatalog(t *testing.T) {
	// 空清单 / 全空名 → 不渲染
	if got, truncated := renderToolsCatalog(nil); got != pCommon.BlankString || truncated {
		t.Fatalf("无工具时不应渲染: got=%q truncated=%v", got, truncated)
	}
	if got, _ := renderToolsCatalog([]hCommon.ToolPromptItem{{Name: "   "}}); got != pCommon.BlankString {
		t.Fatalf("空名工具不应渲染: %q", got)
	}

	// 正常渲染:技能工具带技能名,系统工具带"系统工具",末行是事实源提醒
	got, truncated := renderToolsCatalog([]hCommon.ToolPromptItem{
		{Name: "npa_ingest", Description: "解析材料为按页内容 JSON", Source: hCommon.TSourceSkill, SkillId: "npa-report"},
		{Name: "read", Description: "读取本地文件", Source: hCommon.TSourceSystem},
	})
	if truncated {
		t.Fatalf("未超限不应标记截断")
	}
	for _, want := range []string{
		"- **npa_ingest**(技能 npa-report) — 解析材料为按页内容 JSON",
		"- **read**(系统工具) — 读取本地文件",
		toolsCatalogFooter,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("清单缺少 %q:\n%s", want, got)
		}
	}

	// 单条描述超限 → 截断到上限并加省略号。
	// 这是**节选**而不是内容丢失(完整描述仍在 schema 里) ⇒ **不得**标记 truncated:
	// 否则每个 agent 每轮都会看到一条误导性的 Bootstrap Context Notice(实测踩过)。
	long := strings.Repeat("甲", hCommon.MaxToolDescPromptRunes+50)
	got, truncated = renderToolsCatalog([]hCommon.ToolPromptItem{{Name: "x", Description: long}})
	if truncated {
		t.Fatalf("仅描述被节选不应标记截断(权威定义仍在 schema 里)")
	}
	if !strings.Contains(got, strings.Repeat("甲", hCommon.MaxToolDescPromptRunes)+"…") {
		t.Fatalf("描述应按上限截断且可见: %q", got)
	}

	// 总量超限 → 保前丢后,且结果不超上限
	many := make([]hCommon.ToolPromptItem, 0, 200)
	for i := 0; i < 200; i++ {
		many = append(many, hCommon.ToolPromptItem{
			Name:        fmt.Sprintf("tool_%03d", i),
			Description: strings.Repeat("甲", hCommon.MaxToolDescPromptRunes),
		})
	}
	got, truncated = renderToolsCatalog(many)
	if !truncated {
		t.Fatalf("总量超限应标记截断")
	}
	if runes := len([]rune(got)); runes > hCommon.MaxToolsPromptChars {
		t.Fatalf("清单超过上限: %d > %d", runes, hCommon.MaxToolsPromptChars)
	}
	if !strings.Contains(got, "tool_000") {
		t.Fatalf("应从前往后保留(前面的工具优先): %q", got)
	}
	if strings.Contains(got, "tool_199") {
		t.Fatalf("尾部工具应被丢弃")
	}
}

// TestAssembleSystemPromptToolsSection 验证段位与顺序:
// 工具清单必须被渲染成 "## Available Tools",且排在 Skill 正文之前。
//
// 这里刻意不预加载启动文档(未在 prompt repo 里的 agentId → 无启动文档段):
// 被测的是"工具清单段"本身,启动文档段由 prompt_repo 的测试覆盖。
func TestAssembleSystemPromptToolsSection(t *testing.T) {
	agent := dataobject.TAgent{AgentId: "test_agent_available_tools_section"}
	skills := []hCommon.SkillDocument{{
		SkillId:     "demo",
		SkillFile:   "/s/demo/SKILL.md",
		Content:     "## 执行 SOP\n1. 先调 read",
		FrontMatter: hCommon.SkillFrontMatter{Name: "demo"},
	}}
	tools := []hCommon.ToolPromptItem{
		{Name: "read", Description: "读取本地文件", Source: hCommon.TSourceSystem},
		{Name: "npa_ingest", Description: "解析材料", Source: hCommon.TSourceSkill, SkillId: "npa-report"},
	}

	sysPrompt, err := assembleSystemPrompt(agent, hCommon.BootstrapScopeNormal, []string{"/s/demo/SKILL.md"}, skills, tools)
	if err != nil {
		t.Fatalf("组装 system prompt 失败: %s", err.Error())
	}

	toolsAt := strings.Index(sysPrompt.Prompt, "## Available Tools")
	skillAt := strings.Index(sysPrompt.Prompt, "## Skill: demo")
	if toolsAt < 0 {
		t.Fatalf("缺少 Available Tools 段:\n%s", sysPrompt.Prompt)
	}
	if skillAt < 0 {
		t.Fatalf("缺少 Skill 正文段:\n%s", sysPrompt.Prompt)
	}
	if toolsAt > skillAt {
		t.Fatalf("工具清单应排在 Skill 正文之前(tools=%d skill=%d)", toolsAt, skillAt)
	}
	if !strings.Contains(sysPrompt.Prompt, "- **read**(系统工具) — 读取本地文件") {
		t.Fatalf("清单内容不正确:\n%s", sysPrompt.Prompt)
	}

	// 无工具时该段整体省略(不产生空段)。
	// 注意断言的是**段标题** `## Available Tools`:固定尾巴里的工具纪律会提到
	// "`Available Tools` 段"这几个字,用裸短语判断会永远为真。
	sysPrompt, err = assembleSystemPrompt(agent, hCommon.BootstrapScopeNormal, nil, nil, nil)
	if err != nil {
		t.Fatalf("组装 system prompt 失败: %s", err.Error())
	}
	if strings.Contains(sysPrompt.Prompt, "## Available Tools") {
		t.Fatalf("无工具时不应有 Available Tools 段:\n%s", sysPrompt.Prompt)
	}
}

// TestAssembleSystemPromptToolUsagePolicy 验证"工具调用纪律"是**固定尾巴**:
// 有工具、无工具两种情况下都必须出现 —— 不随 `Available Tools` 段是否渲染而变,也不受预算截断。
func TestAssembleSystemPromptToolUsagePolicy(t *testing.T) {
	agent := dataobject.TAgent{AgentId: "test_agent_tool_usage_policy"}

	withTools, err := assembleSystemPrompt(agent, hCommon.BootstrapScopeNormal, nil, nil,
		[]hCommon.ToolPromptItem{{Name: "read", Description: "读取本地文件", Source: hCommon.TSourceSystem}})
	if err != nil {
		t.Fatalf("组装 system prompt 失败: %s", err.Error())
	}
	if !strings.Contains(withTools.Prompt, "## 工具调用") {
		t.Fatalf("缺少工具调用纪律尾巴:\n%s", withTools.Prompt)
	}

	noTools, err := assembleSystemPrompt(agent, hCommon.BootstrapScopeNormal, nil, nil, nil)
	if err != nil {
		t.Fatalf("组装 system prompt 失败: %s", err.Error())
	}
	if strings.Contains(noTools.Prompt, "## Available Tools") { // 段标题,不是尾巴里提到的裸短语
		t.Fatalf("无工具时不应有 Available Tools 段:\n%s", noTools.Prompt)
	}
	if !strings.Contains(noTools.Prompt, "## 工具调用") {
		t.Fatalf("无工具时也必须带工具调用纪律尾巴(该段可能整体省略,纪律不能跟着丢):\n%s", noTools.Prompt)
	}
}
