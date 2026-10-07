/**
* 稳定前缀"顺序契约"的护栏测试。
*
* 护栏要证明两件事:
*  1) 真实组装出来的 system prompt **不含**任何易变标记(否则其后的整段历史缓存会每轮失效);
*  2) 一旦有人把易变内容写回 system,护栏必须能检出来(否则它只是摆设)。
**/

package ctxmgr

import (
	"strings"
	"testing"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/model/dataobject"
)

// TestValidateStablePrompt 验证护栏本身:合法内容放行,各类易变内容必须命中。
func TestValidateStablePrompt(t *testing.T) {
	// 合法:只有稳定段 + 常量尾巴(尾巴里对 <历史摘要> 的"说明文字"不应被误判)
	legal := "## AGENTS.md\n\n规则\n\n## Available Skills\n\n- **demo**\n\n" +
		"## 上下文块\n\n- <历史摘要>:更早对话的压缩摘要\n"
	if v := ValidateStablePrompt(legal); len(v) != 0 {
		t.Fatalf("合法稳定前缀不应报违规: %v", v)
	}

	cases := map[string]string{
		"当前时间":  "## 当前时间\n\n2026-10-07 10:00:00",
		"历史摘要":  "## 历史摘要\n\n- 更早的对话被压缩在这里",
		"技能正文":  "## Skill: demo\n\n1. 先调 read",
		"记忆自动区": "<!-- auto-memory:updated=x;watermarks=s:1 -->\n- 事实\n<!-- /auto-memory -->",
	}
	for name, prompt := range cases {
		if v := ValidateStablePrompt(prompt); len(v) == 0 {
			t.Errorf("%s 应被判定为易变内容(应后置注入,不得进 system)", name)
		}
	}
}

// TestAssembleSystemPromptPassesGuard 用**真实组装结果**验证分工:
// 命中技能的正文必须落在 DynamicSkills(后置),稳定前缀里不能有它。
func TestAssembleSystemPromptPassesGuard(t *testing.T) {
	agent := dataobject.TAgent{AgentId: "test_agent_guard_pass"}
	skills := []hCommon.SkillDocument{{
		SkillId:     "demo",
		SkillFile:   "/s/demo/SKILL.md",
		Content:     "## 执行 SOP\n1. 先调 read",
		FrontMatter: hCommon.SkillFrontMatter{Name: "demo", Description: "演示技能"},
	}}

	sp, err := assembleSystemPrompt(agent, hCommon.BootstrapScopeNormal, []string{"/s/demo/SKILL.md"}, skills, nil)
	if err != nil {
		t.Fatalf("组装失败: %s", err.Error())
	}
	if v := ValidateStablePrompt(sp.Prompt); len(v) > 0 {
		t.Fatalf("稳定前缀含易变内容: %v\n%s", v, sp.Prompt)
	}
	if !strings.Contains(sp.DynamicSkills, "## Skill: demo") {
		t.Fatalf("命中技能的正文应落在 DynamicSkills: %q", sp.DynamicSkills)
	}
}

// TestPromptStableSectionOrder 断言稳定段顺序符合契约(人工文件 → 配置清单)。
func TestPromptStableSectionOrder(t *testing.T) {
	agent := dataobject.TAgent{AgentId: "test_agent_section_order"}
	skills := []hCommon.SkillDocument{{
		SkillId:     "demo",
		SkillFile:   "/s/demo/SKILL.md",
		FrontMatter: hCommon.SkillFrontMatter{Name: "demo", Description: "演示技能"},
	}}
	tools := []hCommon.ToolPromptItem{{Name: "read", Description: "读取本地文件", Source: hCommon.TSourceSystem}}

	sp, err := assembleSystemPrompt(agent, hCommon.BootstrapScopeNormal, nil, skills, tools)
	if err != nil {
		t.Fatalf("组装失败: %s", err.Error())
	}

	prev, present := -1, 0
	for _, title := range promptStableSectionOrder {
		at := strings.Index(sp.Prompt, sectionTitlePrefix+title)
		if at < 0 {
			continue // 该段本轮不存在(可选文件缺失 / 无工具)
		}
		if at < prev {
			t.Fatalf("段顺序违反契约:%q 出现在前一段之前\n%s", title, sp.Prompt)
		}
		prev = at
		present++
	}
	if present < 2 {
		t.Fatalf("样本段过少(%d),未真正校验顺序", present)
	}
}
