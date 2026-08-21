/**
* PromptService 与 PromptRepository 的集成测试
**/

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/model/repo"
	"my.favorite/cclawservice/utility"
)

// writeFile 创建测试工作区文件
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir error:%s", err.Error())
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file error:%s", err.Error())
	}
}

func TestSplitFrontMatter(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantFM   string
		wantBody string
		hasFront bool
	}{
		{"with front matter", "---\nname: test\ndescription: 'hello'\n---\n# Body", "name: test\ndescription: 'hello'", "# Body", true},
		{"no front matter", "# Just body", "", "# Just body", false},
		{"unclosed delimiter", "---\nname: test\n# Body", "", "---\nname: test\n# Body", false},
		{"crlf", "---\r\nname: test\r\n---\r\n# Body", "name: test", "# Body", true},
		{"bom prefix", "\ufeff---\nname: test\n---\n# Body", "name: test", "# Body", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fm, body := utility.SplitFrontMatter([]byte(c.input))
			if c.hasFront && strings.TrimSpace(fm) != c.wantFM {
				t.Errorf("front matter mismatch: got=%q want=%q", fm, c.wantFM)
			}
			if !c.hasFront && fm != "" {
				t.Errorf("expected empty front matter, got=%q", fm)
			}
			if strings.TrimSpace(body) != c.wantBody {
				t.Errorf("body mismatch: got=%q want=%q", body, c.wantBody)
			}
		})
	}
}

func TestBuildSystemPrompt(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "AGENTS.md"),
		"---\nsummary: \"rules\"\ntitle: \"AGENTS.md\"\nread_when:\n  - Session startup\n---\n# Rules\n- Read before edit.\n")
	writeFile(t, filepath.Join(root, "SOUL.md"),
		"---\nsummary: \"persona\"\ntitle: \"SOUL.md\"\n---\n# Soul\nBe direct and technical.\n")
	writeFile(t, filepath.Join(root, "TOOLS.md"), "# Tools\nUse read/write tools.\n")

	// skill 放在工作区外的独立目录,模拟通过 TSkill.SkillFile 指向
	skillDir := t.TempDir()
	skillFile := filepath.Join(skillDir, "SKILL.md")
	writeFile(t, skillFile,
		"---\nname: test-skill\nuser-invocable: false\ndescription: 'A test skill for validation'\n---\n# Skill Body\nDo the thing.\n")

	promptService := GetPromptService()
	prompt, err := promptService.BuildSystemPrompt("agent-1", root, []string{skillFile})
	if err != nil {
		t.Fatalf("BuildSystemPrompt error:%s", err.Error())
	}

	if prompt.AgentId != "agent-1" {
		t.Errorf("agentId mismatch: got=%q", prompt.AgentId)
	}
	if len(prompt.Sections) != 4 {
		t.Fatalf("expected 4 sections(agent/soul/tools/skills), got=%d", len(prompt.Sections))
	}

	full := prompt.Prompt
	for _, want := range []string{"# Rules", "Read before edit.", "Be direct and technical.", "Use read/write tools.", "test-skill", "A test skill for validation"} {
		if !strings.Contains(full, want) {
			t.Errorf("prompt missing expected content:%q", want)
		}
	}

	// 顺序:agent -> soul -> tools -> skills
	if strings.Index(full, "# Rules") > strings.Index(full, "# Soul") {
		t.Errorf("agent section should precede soul section")
	}
	if strings.Index(full, "Available Skills") < strings.Index(full, "# Tools") {
		t.Errorf("skills section should follow tools section")
	}

	// 幂等 + 缓存:再次构建应成功且结果一致
	prompt2, err := promptService.BuildSystemPrompt("agent-1", root, []string{skillFile})
	if err != nil {
		t.Fatalf("second BuildSystemPrompt error:%s", err.Error())
	}
	if prompt2.Prompt != prompt.Prompt {
		t.Errorf("cached prompt mismatch: got=%d chars want=%d chars", len(prompt2.Prompt), len(prompt.Prompt))
	}

	// JSON 序列化可用
	jsonStr, err := prompt.ToJsonString()
	if err != nil {
		t.Fatalf("ToJsonString error:%s", err.Error())
	}
	if !strings.Contains(jsonStr, "agent-1") {
		t.Errorf("json missing agentId:%s", jsonStr)
	}
}

func TestBuildSystemPromptMissingRequired(t *testing.T) {
	root := t.TempDir()
	// 只有 SOUL.md,缺必需的 AGENTS.md —— 应注入 [MISSING] 标记,不应报错
	writeFile(t, filepath.Join(root, "SOUL.md"), "# Soul only")

	promptService := GetPromptService()
	prompt, err := promptService.BuildSystemPrompt("agent-2", root, nil)
	if err != nil {
		t.Fatalf("BuildSystemPrompt error:%s", err.Error())
	}

	// AGENTS.md(必需,缺失→[MISSING]标记) + SOUL.md = 2 节
	if len(prompt.Sections) != 2 {
		t.Fatalf("expected 2 sections(AGENTS missing marker + SOUL), got=%d", len(prompt.Sections))
	}
	if !strings.Contains(prompt.Prompt, "[MISSING]") {
		t.Errorf("expected [MISSING] marker in prompt")
	}
	if !strings.Contains(prompt.Prompt, "AGENTS.md") {
		t.Errorf("missing marker should mention AGENTS.md")
	}
}

func TestBuildSystemPromptScopeFiltering(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "AGENTS.md"), "# Rules")
	writeFile(t, filepath.Join(root, "SOUL.md"), "# Soul")
	writeFile(t, filepath.Join(root, "IDENTITY.md"), "# Identity")
	writeFile(t, filepath.Join(root, "USER.md"), "# User")
	writeFile(t, filepath.Join(root, "MEMORY.md"), "# Memory")

	promptService := GetPromptService()

	// subagent: 仅 AGENTS.md,不继承父 agent 的人格/用户/记忆
	sub, err := promptService.BuildSystemPromptEx("agent-sub", root, nil, dataobject.BootstrapScopeSubagent)
	if err != nil {
		t.Fatalf("subagent error:%s", err.Error())
	}
	if !strings.Contains(sub.Prompt, "# Rules") {
		t.Errorf("subagent should contain AGENTS.md")
	}
	for _, excluded := range []string{"# Soul", "# Identity", "# User", "# Memory"} {
		if strings.Contains(sub.Prompt, excluded) {
			t.Errorf("subagent should not contain %q", excluded)
		}
	}

	// cron: AGENTS/SOUL/IDENTITY/USER,不含 MEMORY.md(隐私)
	cron, err := promptService.BuildSystemPromptEx("agent-cron", root, nil, dataobject.BootstrapScopeCron)
	if err != nil {
		t.Fatalf("cron error:%s", err.Error())
	}
	if !strings.Contains(cron.Prompt, "# Rules") || !strings.Contains(cron.Prompt, "# Soul") {
		t.Errorf("cron should contain AGENTS and SOUL")
	}
	if strings.Contains(cron.Prompt, "# Memory") {
		t.Errorf("cron should not contain MEMORY.md")
	}

	// heartbeat: 不注入启动文件
	hb, err := promptService.BuildSystemPromptEx("agent-hb", root, nil, dataobject.BootstrapScopeHeartbeat)
	if err != nil {
		t.Fatalf("heartbeat error:%s", err.Error())
	}
	if strings.Contains(hb.Prompt, "# Rules") {
		t.Errorf("heartbeat should not inject bootstrap files")
	}
}

func TestBuildSystemPromptTruncatesOversizeDoc(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "AGENTS.md"), "# Big\n"+strings.Repeat("a", 25000))

	promptService := GetPromptService()
	prompt, err := promptService.BuildSystemPrompt("agent-5", root, nil)
	if err != nil {
		t.Fatalf("BuildSystemPrompt error:%s", err.Error())
	}
	if len(prompt.Truncated) == 0 {
		t.Errorf("expected AGENTS.md to be marked truncated")
	}
	if len(prompt.Prompt) > 21000 {
		t.Errorf("prompt should be truncated to per-file budget, len=%d", len(prompt.Prompt))
	}
	if !strings.Contains(prompt.Prompt, "Bootstrap Context Notice") {
		t.Errorf("expected truncation notice section")
	}
}

func TestBuildSystemPromptSkillVersion(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "AGENTS.md"), "# Rules")

	skillDir := t.TempDir()
	skillFile := filepath.Join(skillDir, "SKILL.md")
	writeFile(t, skillFile,
		"---\nname: vskill\nversion: 1.2.0\ndescription: 'A versioned skill'\n---\n# SECRET BODY\n")

	promptService := GetPromptService()
	prompt, err := promptService.BuildSystemPrompt("agent-6", root, []string{skillFile})
	if err != nil {
		t.Fatalf("BuildSystemPrompt error:%s", err.Error())
	}

	if !strings.Contains(prompt.Prompt, "vskill") || !strings.Contains(prompt.Prompt, "1.2.0") {
		t.Errorf("skill version should be rendered in the catalog")
	}
	if strings.Contains(prompt.Prompt, "SECRET BODY") {
		t.Errorf("skill body should NOT be injected, only metadata")
	}
}

func TestBuildSystemPromptRejectsNonSkillFile(t *testing.T) {
	root := t.TempDir()

	// 先加载工作区,保证 skill 相对路径可解析
	promptService := GetPromptService()
	if _, err := promptService.BuildSystemPrompt("agent-3", root, nil); err != nil {
		t.Fatalf("build workspace error:%s", err.Error())
	}

	// 非法:skill 文件名必须为 SKILL.md
	err := repo.GetPromptRepository().AddSkill("bad", filepath.Join(t.TempDir(), "not_a_skill.md"))
	if err == nil {
		t.Fatalf("expected non-SKILL.md file to be rejected")
	}
}

func TestBuildSystemPromptRejectsOversizeFile(t *testing.T) {
	root := t.TempDir()

	promptService := GetPromptService()
	if _, err := promptService.BuildSystemPrompt("agent-4", root, nil); err != nil {
		t.Fatalf("build workspace error:%s", err.Error())
	}

	// 非法:超过单文件大小上限
	hugeFile := filepath.Join(t.TempDir(), "SKILL.md")
	writeFile(t, hugeFile, strings.Repeat("x", 300*1024)) // > 256KB

	err := repo.GetPromptRepository().AddSkill("huge", hugeFile)
	if err == nil {
		t.Fatalf("expected oversize file to be rejected")
	}
}
