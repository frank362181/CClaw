package ctxmgr

import (
	"strings"
	"testing"

	hCommon "my.favorite/cclawservice/harness/common"
)

// TestRenderActiveSkillDocs 锁定“只注入命中技能的正文”这条规则:
//   - 命中 → 渲染成 section(标题 Skill: <name>),内容为 SKILL.md 正文;
//   - 未命中 / active 为空 → 一个都不渲染(避免无关 SOP 稀释注意力);
//   - 单技能正文超限 → 截断并把技能名记入 truncated(会落成 Bootstrap Context Notice)。
func TestRenderActiveSkillDocs(t *testing.T) {
	skills := []hCommon.SkillDocument{
		{
			SkillId:     "weather",
			SkillFile:   "/s/weather/SKILL.md",
			Content:     "## 执行 SOP\n1. 必须实时调用 get_weather",
			FrontMatter: hCommon.SkillFrontMatter{Name: "weather"},
		},
		{
			SkillId:     "npa-report",
			SkillFile:   "/s/npa-report/SKILL.md",
			Content:     "## 核心工作流程\n1. npa_ingest 只传单个文件",
			FrontMatter: hCommon.SkillFrontMatter{Name: "npa-report"},
		},
	}

	got := renderActiveSkillDocs(skills, []string{"  /s/npa-report/SKILL.md  "}, nil)
	if len(got) != 1 || got[0].Title != "Skill: npa-report" || !strings.Contains(got[0].Content, "只传单个文件") {
		t.Fatalf("只应渲染命中的技能: %+v", got)
	}

	if got := renderActiveSkillDocs(skills, nil, nil); len(got) != 0 {
		t.Fatalf("active 为空时不应渲染任何正文: %+v", got)
	}

	big := []hCommon.SkillDocument{{
		SkillId:   "big",
		SkillFile: "/s/big/SKILL.md",
		Content:   strings.Repeat("甲", MaxActiveSkillDocRunes+100),
	}}
	var truncated []string
	got = renderActiveSkillDocs(big, []string{"/s/big/SKILL.md"}, &truncated)
	if len(got) != 1 || len([]rune(got[0].Content)) > MaxActiveSkillDocRunes {
		t.Fatalf("超限必须截断: runes=%d", len([]rune(got[0].Content)))
	}
	if len(truncated) != 1 {
		t.Fatalf("截断必须可见(记入 truncated): %v", truncated)
	}
}
