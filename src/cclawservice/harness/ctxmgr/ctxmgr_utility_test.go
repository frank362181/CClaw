/**
* 技能文档路径解析(ResolveSkillDocPath / BuildSkillRoot / NormalizeSkillDir)的单元测试。
*
* 从 harness/common 迁入:这些函数随"仓库/缓存分离"一起移到了 ctxmgr,
* 而 harness/common 不能反向 import ctxmgr(会成环),测试必须跟着函数走。
*
* 重点是"越界校验的唯一实现 = SafeJoinWithRoot":三种入参形式必须落到同一个绝对路径,
* 非法/越界/文件名不符必须被拒,且目录名分支与绝对路径分支用的是同一个真实根基准。
*
* 约定:SKILL.md 必须落在 <exeDir>/cclaw/skills/<skill>/ 之下(BuildSkillRoot 的基准),
* go test 的 exeDir 是临时目录,可以直接写入,用例结束由 t.Cleanup 删除。
**/

package ctxmgr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSkillDoc 在技能根下建一个技能并写入 SKILL.md,返回 SKILL.md 绝对路径。
func writeSkillDoc(t *testing.T, skillName string) string {
	t.Helper()

	skillRoot, ok := BuildSkillRoot()
	if !ok || len(skillRoot) == 0 {
		t.Fatalf("skill root is unavailable")
	}

	skillDir := filepath.Join(skillRoot, skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir error:%s", err.Error())
	}
	t.Cleanup(func() { _ = os.RemoveAll(skillDir) })

	docPath := filepath.Join(skillDir, SkillDocFileName)
	if err := os.WriteFile(docPath, []byte("---\nname: ut\n---\n# body\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md error:%s", err.Error())
	}

	return docPath
}

// TestResolveSkillDocPathAcceptsThreeForms 三种入参形式(目录名/历史文档路径/已解析绝对路径)
// 必须解析成同一个绝对路径 —— 单条解析与批量解析走的是同一套规则。
func TestResolveSkillDocPathAcceptsThreeForms(t *testing.T) {
	skillRoot, _ := BuildSkillRoot()
	writeSkillDoc(t, "ut-resolve-skill")

	want := filepath.Join(skillRoot, "ut-resolve-skill", SkillDocFileName)
	for _, input := range []string{
		"ut-resolve-skill",                     // 目录名(推荐)
		"ut-resolve-skill/" + SkillDocFileName, // 历史文档路径
		want,                                   // 已解析的绝对路径
		"  " + want + "  ",                     // 带空白
	} {
		got, err := ResolveSkillDocPath(input)
		if err != nil {
			t.Errorf("input %q should resolve: %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("input %q: got=%q want=%q", input, got, want)
		}
	}
}

// TestResolveSkillDocPathRejectsIllegal 非法/越界/文件名不符必须被拒(拒绝时返回错误而非静默降级)。
func TestResolveSkillDocPathRejectsIllegal(t *testing.T) {
	docPath := writeSkillDoc(t, "ut-reject-skill")

	cases := []struct {
		name  string
		input string
	}{
		{"空入参", ""},
		{"纯空白", "   "},
		{"目录穿越", "../escape"},
		{"多级穿越", "a/../../escape"},
		{"绝对路径但不在技能根内", filepath.Join(os.TempDir(), SkillDocFileName)},
		{"在技能根内但文件名不是 SKILL.md", filepath.Join(filepath.Dir(docPath), "other.md")},
	}

	for _, c := range cases {
		if got, err := ResolveSkillDocPath(c.input); err == nil {
			t.Errorf("%s(%q) should be rejected: got=%q", c.name, c.input, got)
		}
	}

	// 反向确认:合法的绝对路径不该被误拒(SafeJoinWithRoot 要求传真实根,基准必须一致)
	if got, err := ResolveSkillDocPath(docPath); err != nil || !strings.HasSuffix(got, SkillDocFileName) {
		t.Errorf("legal absolute path should be accepted: got=%q err=%v", got, err)
	}
}
