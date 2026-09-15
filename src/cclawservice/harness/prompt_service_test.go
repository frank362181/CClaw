/**
* PromptService 与 PromptRepository 的集成测试
**/

package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	cCommon "my.favorite/cclawservice/common"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/ctxmgr"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/model/repo"
	pCommon "my.favorite/platformutility/common"
)

// TestSkillsCatalogBudgetIsRuneBased 回归(S2):skill 目录的预算按 **rune** 计,不再按字节。
//
// 中文的 UTF-8 字节数是 rune 的 3 倍 —— 按字节卡会让实际额度只剩 1/3,
// 而且额度随中英文比例漂移(同一个常量,英文目录 18000 字、中文目录只有 6000 字)。
func TestSkillsCatalogBudgetIsRuneBased(t *testing.T) {
	skills := make([]dataobject.SkillDocument, 0, 100)
	for i := 0; i < 100; i++ {
		skills = append(skills, dataobject.SkillDocument{
			FrontMatter: dataobject.SkillFrontMatter{
				Name:        fmt.Sprintf("技能%03d", i),
				Version:     "1.0.0",
				Description: strings.Repeat("描述", 50), // 100 rune
			},
		})
	}

	catalog, truncated := renderSkillsCatalog(skills)
	runes, bytes := len([]rune(catalog)), len(catalog)

	if truncated {
		t.Errorf("约 %d rune / %d 字节的目录应落在 %d rune 预算内,不应被截断"+
			"(按字节卡时,中文目录的实际额度只剩 ~1/3)", runes, bytes, hCommon.MaxSkillsPromptChars)
	}
	if runes > hCommon.MaxSkillsPromptChars {
		t.Fatalf("目录 rune 数 %d 超出预算 %d", runes, hCommon.MaxSkillsPromptChars)
	}
	if bytes <= hCommon.MaxSkillsPromptChars {
		t.Fatalf("用例前提不成立:本 fixture 应处于「字节超限、rune 未超限」区间(bytes=%d runes=%d)",
			bytes, runes)
	}
}

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

// agentWorkspaceRoot 返回测试 agent 的启动文档根目录(<exeDir>/cclaw/<ownerId>/<agentDir>)。
// 与生产路径 BuildAgentBootstrapRoot 保持一致,prompt 组装才能命中真实文件系统。
func agentWorkspaceRoot(ownerId, agentDir string) string {
	return filepath.Join(pCommon.GetExecutableDir(), cCommon.AgentBootstrapDir, ownerId, agentDir)
}

// newTestAgent 在 <exeDir>/cclaw/<ownerId>/<agentDir> 下创建测试工作区(目录必须存在),
// 通过 fn 写入启动文档,返回对应的 TAgent;测试结束自动清理。
func newTestAgent(t *testing.T, ownerId, agentId, agentDir string, fn func(root string)) dataobject.TAgent {
	t.Helper()
	root := agentWorkspaceRoot(ownerId, agentDir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir workspace error:%s", err.Error())
	}
	if fn != nil {
		fn(root)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(root)
	})
	return dataobject.TAgent{
		OwnerId:  ownerId,
		AgentId:  agentId,
		AgentDir: agentDir,
	}
}

// writeSkillFile 在独立临时目录创建 SKILL.md(工作区外,模拟 TSkill.SkillFile 指向)。
func writeSkillFile(t *testing.T, content string) string {
	t.Helper()
	skillDir := t.TempDir()
	skillFile := filepath.Join(skillDir, "SKILL.md")
	writeFile(t, skillFile, content)
	return skillFile
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
			fm, body := hCommon.SplitFrontMatter([]byte(c.input))
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
	agent := newTestAgent(t, "t-owner", "agent-1", "agent-1", func(root string) {
		writeFile(t, filepath.Join(root, "AGENTS.md"),
			"---\nsummary: \"rules\"\ntitle: \"AGENTS.md\"\nread_when:\n  - Session startup\n---\n# Rules\n- Read before edit.\n")
		writeFile(t, filepath.Join(root, "SOUL.md"),
			"---\nsummary: \"persona\"\ntitle: \"SOUL.md\"\n---\n# Soul\nBe direct and technical.\n")
		writeFile(t, filepath.Join(root, "TOOLS.md"), "# Tools\nUse read/write tools.\n")
	})

	// skill 放在工作区外的独立目录,模拟通过 TSkill.SkillFile 指向
	skillFile := writeSkillFile(t,
		"---\nname: test-skill\nuser-invocable: false\ndescription: 'A test skill for validation'\n---\n# Skill Body\nDo the thing.\n")

	promptService := GetPromptService()
	prompt, err := promptService.BuildSystemPrompt(agent, []string{skillFile})
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
	prompt2, err := promptService.BuildSystemPrompt(agent, []string{skillFile})
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
	// 只有 SOUL.md,缺必需的 AGENTS.md —— 应注入 [MISSING] 标记,不应报错
	agent := newTestAgent(t, "t-owner", "agent-2", "agent-2", func(root string) {
		writeFile(t, filepath.Join(root, "SOUL.md"), "# Soul only")
	})

	promptService := GetPromptService()
	prompt, err := promptService.BuildSystemPrompt(agent, nil)
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
	agent := newTestAgent(t, "t-owner", "agent-scope", "agent-scope", func(root string) {
		writeFile(t, filepath.Join(root, "AGENTS.md"), "# Rules")
		writeFile(t, filepath.Join(root, "SOUL.md"), "# Soul")
		writeFile(t, filepath.Join(root, "IDENTITY.md"), "# Identity")
		writeFile(t, filepath.Join(root, "USER.md"), "# User")
		writeFile(t, filepath.Join(root, "MEMORY.md"), "# Memory")
	})

	promptService := GetPromptService()

	// subagent: 仅 AGENTS.md,不继承父 agent 的人格/用户/记忆
	sub, err := promptService.BuildSystemPromptEx(agent, nil, hCommon.BootstrapScopeSubagent)
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
	cron, err := promptService.BuildSystemPromptEx(agent, nil, hCommon.BootstrapScopeCron)
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
	hb, err := promptService.BuildSystemPromptEx(agent, nil, hCommon.BootstrapScopeHeartbeat)
	if err != nil {
		t.Fatalf("heartbeat error:%s", err.Error())
	}
	if strings.Contains(hb.Prompt, "# Rules") {
		t.Errorf("heartbeat should not inject bootstrap files")
	}
}

func TestBuildSystemPromptTruncatesOversizeDoc(t *testing.T) {
	agent := newTestAgent(t, "t-owner", "agent-5", "agent-5", func(root string) {
		writeFile(t, filepath.Join(root, "AGENTS.md"), "# Big\n"+strings.Repeat("a", 25000))
	})

	promptService := GetPromptService()
	prompt, err := promptService.BuildSystemPrompt(agent, nil)
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
	agent := newTestAgent(t, "t-owner", "agent-6", "agent-6", func(root string) {
		writeFile(t, filepath.Join(root, "AGENTS.md"), "# Rules")
	})

	skillFile := writeSkillFile(t,
		"---\nname: vskill\nversion: 1.2.0\ndescription: 'A versioned skill'\n---\n# SECRET BODY\n")

	promptService := GetPromptService()
	prompt, err := promptService.BuildSystemPrompt(agent, []string{skillFile})
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
	// 空工作区(目录存在即可),先加载保证 agent 已缓存
	agent := newTestAgent(t, "t-owner", "agent-3", "agent-3", nil)

	// 先加载工作区,保证 skill 相对路径可解析
	promptService := GetPromptService()
	if _, err := promptService.BuildSystemPrompt(agent, nil); err != nil {
		t.Fatalf("build workspace error:%s", err.Error())
	}

	// 非法:skill 文件名必须为 SKILL.md
	err := repo.GetPromptRepository().AddSkill("agent-3", "bad", filepath.Join(t.TempDir(), "not_a_skill.md"))
	if err == nil {
		t.Fatalf("expected non-SKILL.md file to be rejected")
	}
}

func TestBuildSystemPromptRejectsOversizeFile(t *testing.T) {
	agent := newTestAgent(t, "t-owner", "agent-4", "agent-4", nil)

	promptService := GetPromptService()
	if _, err := promptService.BuildSystemPrompt(agent, nil); err != nil {
		t.Fatalf("build workspace error:%s", err.Error())
	}

	// 非法:超过单文件大小上限
	hugeFile := filepath.Join(t.TempDir(), "SKILL.md")
	writeFile(t, hugeFile, strings.Repeat("x", 300*1024)) // > 256KB

	err := repo.GetPromptRepository().AddSkill("agent-4", "huge", hugeFile)
	if err == nil {
		t.Fatalf("expected oversize file to be rejected")
	}
}

// TestTruncateTailBudgetAccountsForTitles 回归(S4):总预算必须把标题与段间分隔一起记账。
//
// 修复前只累计正文,于是"预算 61440"实际会写出含标题的更长 prompt;
// 关键设计是**记账口径与最终拼接共用同一组常量**(sectionOverheadRunes),
// 以后改分隔符不会再出现"记账一套、渲染另一套"。
func TestTruncateTailBudgetAccountsForTitles(t *testing.T) {
	sections := []dataobject.SystemPromptSection{
		{Title: "AGENTS.md", Content: strings.Repeat("甲", 500)},
		{Title: "SOUL.md", Content: strings.Repeat("乙", 500)},
		{Title: "TOOLS.md", Content: strings.Repeat("丙", 500)},
	}

	rendered := func(list []dataobject.SystemPromptSection) int {
		total := 0
		for i := range list {
			total += len([]rune(list[i].Content)) + sectionOverheadRunes(list[i], i == 0)
		}
		return total
	}

	// 预算取"两份正文 + 一点余量":修复前会把第三份也按正文预算截出一截,渲染总长超预算
	const cap = 1100
	kept, truncated := truncateTailBudget(sections, cap, nil)

	if got := rendered(kept); got > cap {
		t.Errorf("渲染后的总占用 %d 超出预算 %d(记账必须含标题/分隔开销)", got, cap)
	}
	if got := rendered(kept); cap-got > 64 {
		t.Errorf("渲染后只用了 %d/%d,预算未被填满", got, cap)
	}
	if len(truncated) == 0 {
		t.Errorf("预算不足时应记录被截断/丢弃的 section: %v", truncated)
	}

	// 标题开销必须真的算进去:同样正文、带标题比不带标题更早触顶
	unstyled := []dataobject.SystemPromptSection{
		{Content: strings.Repeat("甲", 500)},
		{Content: strings.Repeat("乙", 500)},
		{Content: strings.Repeat("丙", 500)},
	}
	want := 3*len(sectionSeparator) + 3*len(sectionTitlePrefix) + len("AGENTS.md") + len("SOUL.md") + len("TOOLS.md")
	if diff := rendered(sections) - rendered(unstyled); diff != want {
		t.Errorf("标题/分隔开销计算不符: styled=%d plain=%d diff=%d want=%d",
			rendered(sections), rendered(unstyled), diff, want)
	}
}

func TestEffectiveBootstrapBudget(t *testing.T) {
	cases := []struct {
		name       string
		configured int
		want       int
	}{
		{"explicit budget", 80000, 80000},
		{"zero falls back to default", 0, hCommon.MaxBootstrapTotalChars},
		// 三态约定(与 max_history_tokens 一致):负数 = 显式不限制 → 返回 0,
		// 调用方(truncateTailBudget)把 cap<=0 解释为"该上限关闭"
		{"negative disables the cap", -1, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ctxmgr.CalcBoostrapBudget(c.configured); got != c.want {
				t.Errorf("effectiveBootstrapBudget(%d)=%d want=%d", c.configured, got, c.want)
			}
		})
	}
}

// TestBootstrapBudgetNegativeMeansUnlimited 三态约定的下半段:负数配置真的能让总上限不生效。
// (以前 0 与负数都回落默认值,于是"我要不限制"根本表达不出来,
// truncateTailBudget 里那句 cap<=0 也就成了生产不可达的死分支。)
func TestBootstrapBudgetNegativeMeansUnlimited(t *testing.T) {
	sections := []dataobject.SystemPromptSection{
		{Title: "AGENTS.md", Content: strings.Repeat("规", hCommon.MaxBootstrapTotalChars*2)},
	}

	kept, truncated := truncateTailBudget(sections, ctxmgr.CalcBoostrapBudget(-1), nil)
	if len(kept) != 1 || len(truncated) != 0 {
		t.Fatalf("负数配置应完全不限: kept=%d truncated=%v", len(kept), truncated)
	}
	if len([]rune(kept[0].Content)) != len([]rune(sections[0].Content)) {
		t.Error("不限制时内容应原样保留")
	}
}

// TestSectionCapIsBackstopOnly 把"capSections 只是兑底层"这个前提显式化。
//
// 这条测试变红只有两种可能:
//   - 有人放开了某个来源上限(那时 section 兑底会真的生效,需要重新定值);
//   - 有人调小了 MaxPromptSectionLen(那时它开始截断正常文档 —— 更要三思)。
func TestSectionCapIsBackstopOnly(t *testing.T) {
	sources := map[string]int{
		"MaxBootstrapFileChars": hCommon.MaxBootstrapFileChars,
		"MaxUserDocChars":       hCommon.MaxUserDocChars,
		"MaxSkillsPromptChars":  hCommon.MaxSkillsPromptChars,
	}
	for name, limit := range sources {
		if limit >= hCommon.MaxPromptSectionLen {
			t.Errorf("%s(%d) 已达到/超过 MaxPromptSectionLen(%d):section 兑底会真的生效,请重新评估该常量",
				name, limit, hCommon.MaxPromptSectionLen)
		}
	}

	// 行为层面:每类 section 都塞到各自来源上限,section 兑底与默认总预算都不应截断
	sections := []dataobject.SystemPromptSection{
		{Title: "AGENTS.md", Content: strings.Repeat("规", hCommon.MaxBootstrapFileChars)},
		{Title: "USER.md", Content: strings.Repeat("用", hCommon.MaxUserDocChars)},
		{Title: "Available Skills", Content: strings.Repeat("技", hCommon.MaxSkillsPromptChars)},
	}

	totalRunes := 0
	for i := range sections {
		totalRunes += len([]rune(sections[i].Content))
	}
	if totalRunes >= hCommon.MaxBootstrapTotalChars {
		t.Errorf("各来源上限之和(%d rune)已达到默认总上限(%d):默认总预算需要重新定值",
			totalRunes, hCommon.MaxBootstrapTotalChars)
	}

	if _, truncated := capSections(sections, hCommon.MaxPromptSectionLen, nil); len(truncated) != 0 {
		t.Errorf("各 section 都在来源上限内,section 兑底不应截断: %v", truncated)
	}
	if _, truncated := truncateTailBudget(sections, ctxmgr.CalcBoostrapBudget(0), nil); len(truncated) != 0 {
		t.Errorf("默认总预算下不应截断: %v", truncated)
	}
}

// TestCharsToTokens 单位换算:rune → token(最坏情况按 CJK 计)。
// 上限用 rune 表达、下游预算用 token 表达,这个换算是二者能比较的唯一桥梁。
func TestCharsToTokens(t *testing.T) {
	if got := ctxmgr.CharsToTokens(0); got != 0 {
		t.Errorf("0 rune 应为 0 token,实际 %d", got)
	}
	if got := ctxmgr.CharsToTokens(-5); got != 0 {
		t.Errorf("负数应归零,实际 %d", got)
	}
	if got := ctxmgr.CharsToTokens(int(ctxmgr.CharsPerCJKToken * 100)); got != 100 {
		t.Errorf("150 rune 应为 100 token,实际 %d", got)
	}
	// 实测口径:61440 rune ≈ 41k token —— 这是 SystemPromptReserveTokens 默认量级的 5 倍
	if got := ctxmgr.CharsToTokens(hCommon.MaxBootstrapTotalChars); got < 40000 || got > 42000 {
		t.Errorf("默认总上限的 token 当量应约 41k,实际 %d", got)
	}
}

func TestCapSectionsTruncatesOversizedSection(t *testing.T) {
	oversize := strings.Repeat("中", hCommon.MaxPromptSectionLen+500)
	sections := []dataobject.SystemPromptSection{
		{Title: "AGENTS.md", Content: "# ok"},
		{Title: "HUGE.md", Content: oversize},
	}

	got, truncated := capSections(sections, hCommon.MaxPromptSectionLen, nil)

	if len(got) != 2 {
		t.Fatalf("section count should be preserved: got=%d", len(got))
	}
	if got[0].Content != "# ok" {
		t.Errorf("section within limit should be untouched: %q", got[0].Content)
	}
	// 空 marker 表示纯截断:结果为恰好 limit 个 rune
	if runes := len([]rune(got[1].Content)); runes != hCommon.MaxPromptSectionLen {
		t.Errorf("oversize section runes=%d want=%d", runes, hCommon.MaxPromptSectionLen)
	}
	if !utf8.ValidString(got[1].Content) {
		t.Errorf("truncated content must stay valid UTF-8")
	}
	if len(truncated) != 1 || truncated[0] != "HUGE.md" {
		t.Errorf("truncated list mismatch: %v", truncated)
	}
}

func TestCapSectionsNoopWithinLimitOrDisabled(t *testing.T) {
	sections := []dataobject.SystemPromptSection{{Title: "A", Content: "short"}}

	got, truncated := capSections(sections, hCommon.MaxPromptSectionLen, nil)
	if len(got) != 1 || got[0].Content != "short" || len(truncated) != 0 {
		t.Errorf("capSections should be a no-op within limit: %v %v", got, truncated)
	}

	// limit<=0 表示不限制
	got, truncated = capSections(sections, 0, nil)
	if len(got) != 1 || got[0].Content != "short" || len(truncated) != 0 {
		t.Errorf("limit<=0 should disable the guard: %v %v", got, truncated)
	}
}

func TestUniqueStringsKeepsFirstOrder(t *testing.T) {
	got := pCommon.UniqueStrings([]string{"AGENTS.md", "SOUL.md", "AGENTS.md", "SOUL.md", "TOOLS.md"})
	want := []string{"AGENTS.md", "SOUL.md", "TOOLS.md"}

	if len(got) != len(want) {
		t.Fatalf("len mismatch: got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got=%q want=%q", i, got[i], want[i])
		}
	}
}

func TestBuildSystemPromptTotalBudgetFallback(t *testing.T) {
	// 未配置 bootstrap_total_chars(0)的 agent:总预算应回落到默认值。
	// 每个文件都在单文件预算内,但四份叠加超过默认总预算,必须被裁剪。
	agent := newTestAgent(t, "t-owner", "agent-budget", "agent-budget", func(root string) {
		writeFile(t, filepath.Join(root, "AGENTS.md"), "# A\n"+strings.Repeat("a", 20000))
		writeFile(t, filepath.Join(root, "SOUL.md"), "# S\n"+strings.Repeat("b", 20000))
		writeFile(t, filepath.Join(root, "TOOLS.md"), "# T\n"+strings.Repeat("c", 20000))
		writeFile(t, filepath.Join(root, "BOOTSTRAP.md"), "# B\n"+strings.Repeat("d", 20000))
	})
	if agent.BootstrapTotalChars != 0 {
		t.Fatalf("precondition: agent must be unconfigured (0)")
	}

	promptService := GetPromptService()
	prompt, err := promptService.BuildSystemPrompt(agent, nil)
	if err != nil {
		t.Fatalf("BuildSystemPrompt error:%s", err.Error())
	}

	// 截断提示 section 是在总预算之后追加的固定说明段(与"输出语言"尾巴同性质),不参与预算。
	// 记账口径 = 最终 prompt 的实际占用(正文 + 标题 + 段间分隔),与 truncateTailBudget 一致
	// (以前只算正文,于是实际 prompt 会比 cap 多出「#section × (标题+分隔)」)。
	injected := 0
	for i := range prompt.Sections {
		if prompt.Sections[i].Title == "Bootstrap Context Notice" {
			continue
		}
		injected += len([]rune(prompt.Sections[i].Content)) + sectionOverheadRunes(prompt.Sections[i], injected == 0)
	}

	// 4 份 20000 rune 的文档,默认总预算 61440:应保留前 3 份、截断第 4 份,且尽量填满预算
	if injected > hCommon.MaxBootstrapTotalChars {
		t.Errorf("injected sections total=%d must not exceed the budget=%d",
			injected, hCommon.MaxBootstrapTotalChars)
	}
	if slack := hCommon.MaxBootstrapTotalChars - injected; slack > 64 {
		t.Errorf("injected sections total=%d, budget=%d: 剩余 %d rune 未被利用(预算应尽量填满)",
			injected, hCommon.MaxBootstrapTotalChars, slack)
	}
	if len(prompt.Truncated) == 0 {
		t.Errorf("expected total-budget truncation to be recorded")
	}
	if !strings.Contains(prompt.Prompt, "Bootstrap Context Notice") {
		t.Errorf("expected truncation notice section")
	}
}
