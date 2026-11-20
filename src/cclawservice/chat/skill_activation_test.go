package chat

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v2"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/ctxmgr"
)

// skillRoot 仓库内技能目录(chat 包在 cclawservice/chat 下)
const skillRoot = "../../bin/cclaw/skills"

// loadRealSkills 读取仓库里真实的 SKILL.md,走与生产同一套 front-matter 解析。
//
// 存在的意义:yaml tag 写错(比如 trigger_keywords 与 trigger-keywords)不会报错,
// 只会让字段静默为空,进而让技能永远激活不了。这个测试是唯一的自动化护栏。
func loadRealSkills(t *testing.T, skillIds ...string) []hCommon.SkillDocument {
	t.Helper()

	docs := make([]hCommon.SkillDocument, 0, len(skillIds))
	for _, skillId := range skillIds {
		path := filepath.Join(skillRoot, skillId, "SKILL.md")

		raw, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("SKILL.md 不可读,跳过: %v", err)
		}

		frontMatter, content := ctxmgr.SplitFrontMatter(raw)

		doc := hCommon.SkillDocument{SkillId: skillId, SkillFile: path, Content: content}
		if e := yaml.Unmarshal([]byte(frontMatter), &doc.FrontMatter); e != nil {
			t.Fatalf("%s 的 front matter 解析失败: %v", skillId, e)
		}

		docs = append(docs, doc)
	}

	return docs
}

// buildBundle 只用技能元数据构造可激活的 bundle,不依赖仓库/数据库。
func buildBundle(docs []hCommon.SkillDocument) ToolsBundle {
	slots := make([]SkillSlot, 0, len(docs))
	for _, doc := range docs {
		slots = append(slots, buildSkillSlot(doc, nil, nil))
	}

	return ToolsBundle{Slots: slots}
}

func containsSkill(skillIds []string, want string) bool {
	for _, skillId := range skillIds {
		if skillId == want {
			return true
		}
	}

	return false
}

// TestWeatherSkillStillActivatable yaml tag 修复的副作用检查:
// weather 的 14 个 trigger-keywords 曾经全部静默丢失,导致该技能彻底不可激活。
func TestWeatherSkillStillActivatable(t *testing.T) {
	docs := loadRealSkills(t, "weather", "npa-report", "web_fetch")

	weather := docs[0]
	if len(weather.FrontMatter.TriggerKeywords) == 0 {
		t.Fatal("weather 的 trigger-keywords 为空:请检查 SkillFrontMatter 的 yaml tag 是否为 trigger-keywords")
	}
	if !weather.FrontMatter.UserInvocable {
		t.Fatal("weather 的 user-invocable 未绑定:请检查 yaml tag 是否为 user-invocable")
	}

	gate := buildBundle(docs).Activate("查询今天杭州的天气")

	if !containsSkill(gate.ActiveSkills, "weather") {
		t.Fatalf("天气问题应激活 weather,实际 active=%v", gate.ActiveSkills)
	}
	if containsSkill(gate.ActiveSkills, "npa-report") {
		t.Fatalf("天气问题不应激活 npa-report,实际 active=%v", gate.ActiveSkills)
	}

	t.Logf("active=%v hit=%v keywords=%d", gate.ActiveSkills, gate.HitKeywords,
		len(weather.FrontMatter.TriggerKeywords))
}

// TestNPAReportSkillActivatable 尽调问题仍然能激活 npa-report,
// 且 sop-out-lines 已正确绑定(plan mode 的前提)。
func TestNPAReportSkillActivatable(t *testing.T) {
	docs := loadRealSkills(t, "npa-report")

	npaReport := docs[0]
	if len(npaReport.FrontMatter.SOPOutLines) == 0 {
		t.Fatal("npa-report 的 sop-out-lines 为空:plan mode 将永不触发(检查 yaml tag)")
	}

	gate := buildBundle(docs).Activate("帮我做 10罗勇兴 的个贷不良资产尽调报告")

	if !containsSkill(gate.ActiveSkills, "npa-report") {
		t.Fatalf("尽调问题应激活 npa-report,实际 active=%v", gate.ActiveSkills)
	}
}
