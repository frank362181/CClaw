/**
* PromptRepository.LoadSkills(SKILL.md 唯一读盘 + 唯一 YAML 解析入口)的单元测试。
*
* 覆盖:首次读盘解析、**指纹命中零读盘**(同尺寸改写 + 还原 mtime)、内容变化后重读、
* 重复绑定去重、目录非法/文件缺失/超大/YAML 损坏的降级行为、Destroy 清缓存。
*
* 约定:
*   - PromptRepository 是进程内单例,每个用例开头必须 Destroy() 复位;
*   - SKILL.md 必须落在 <exeDir>/cclaw/skills/<skill>/ 之下(ResolveSkillDocPath 的基准),
*     go test 的 exeDir 是临时目录,可以直接写入,用例结束由 t.Cleanup 删除。
**/

package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	hCommon "my.favorite/cclawservice/harness/common"
)

// testSkillBody 技能正文:用长度相同的词做"同尺寸改写"探针(第一步/第二步 均为 9 字节)。
const testSkillBody = "# 流程\n第一步:先读档案\n"

// writeTestSkill 在 skills 根下建技能目录并写入 SKILL.md,返回该文件的绝对路径。
func writeTestSkill(t *testing.T, skillName, content string) string {
	t.Helper()

	skillRoot, ok := hCommon.BuildSkillRoot()
	if !ok || len(skillRoot) == 0 {
		t.Fatalf("skill root is unavailable")
	}

	skillDir := filepath.Join(skillRoot, skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir error:%s", err.Error())
	}
	t.Cleanup(func() { _ = os.RemoveAll(skillDir) })

	docPath := filepath.Join(skillDir, hCommon.SkillDocFileName)
	if err := os.WriteFile(docPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md error:%s", err.Error())
	}

	return docPath
}

// skillDocText 拼一份带 front matter 的 SKILL.md。
func skillDocText(name, body string) string {
	return "---\nname: " + name + "\ndescription: 单元测试技能\ntrigger-keywords: [测试]\n---\n" + body
}

// resetPromptRepo 复位单例缓存,避免用例之间互相污染。
func resetPromptRepo(t *testing.T) {
	t.Helper()
	GetPromptRepository().Destroy()
	t.Cleanup(func() { GetPromptRepository().Destroy() })
}

// rewriteSameFingerprint 用**同字节长度**的新内容改写文件并还原 mtime,
// 使文件指纹(size:mtime)保持不变 —— 用于证明"命中缓存 ⇒ 没有读盘"。
func rewriteSameFingerprint(t *testing.T, path, content string) {
	t.Helper()

	stat, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat error:%s", err.Error())
	}

	old, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read error:%s", err.Error())
	}
	if len(content) != len(old) {
		t.Fatalf("probe must keep the same byte size: old=%d new=%d", len(old), len(content))
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("rewrite error:%s", err.Error())
	}
	if err := os.Chtimes(path, stat.ModTime(), stat.ModTime()); err != nil {
		t.Fatalf("chtimes error:%s", err.Error())
	}
}

// TestLoadSkillsReadsAndParses 首次加载:读盘 + front matter 解析 + 正文剥离 front matter。
func TestLoadSkillsReadsAndParses(t *testing.T) {
	resetPromptRepo(t)
	writeTestSkill(t, "ut-skill-basic", skillDocText("ut-basic", testSkillBody))

	docs, report := GetPromptRepository().LoadSkills([]string{"ut-skill-basic"})
	if len(docs) != 1 {
		t.Fatalf("skill count mismatch: got=%d want=1", len(docs))
	}
	// 首次加载:1 个重载、0 个命中、无跳过
	if report.Requested != 1 || report.Loaded != 1 || report.Reloaded != 1 ||
		report.Cached != 0 || report.Duplicated != 0 || len(report.Skipped) != 0 {
		t.Errorf("unexpected report on first load: %+v", report)
	}

	doc := docs[0]
	if doc.SkillId != "ut-skill-basic" {
		t.Errorf("skillId should be the skill dir name: got=%q", doc.SkillId)
	}
	if doc.FrontMatter.Name != "ut-basic" {
		t.Errorf("front matter not parsed: got name=%q", doc.FrontMatter.Name)
	}
	if len(doc.FrontMatter.TriggerKeywords) != 1 || doc.FrontMatter.TriggerKeywords[0] != "测试" {
		t.Errorf("trigger keywords not parsed: %v", doc.FrontMatter.TriggerKeywords)
	}
	if strings.TrimSpace(doc.Content) != strings.TrimSpace(testSkillBody) {
		t.Errorf("content should be the body without front matter: got=%q", doc.Content)
	}
	if strings.Contains(doc.Content, "trigger-keywords") {
		t.Errorf("front matter leaked into content: %q", doc.Content)
	}
	if len(doc.Fingerprint) == 0 || doc.ByteSize <= 0 {
		t.Errorf("fingerprint/byteSize should be filled: fp=%q size=%d", doc.Fingerprint, doc.ByteSize)
	}

	// 缓存只存一份(path → doc)
	repo := promptRepository
	repo.mu.Lock()
	cached := len(repo.skillCache)
	repo.mu.Unlock()
	if cached != 1 {
		t.Errorf("skill cache entry count mismatch: got=%d want=1", cached)
	}
}

// TestLoadSkillsCacheHitSkipsDiskRead 指纹命中即复用:同尺寸改写 + 还原 mtime ⇒ 仍返回旧正文。
// 这条是"每轮零读盘"的核心不变量 —— 一旦被人改成"先读后 stat"或去掉缓存,本用例立刻失败。
func TestLoadSkillsCacheHitSkipsDiskRead(t *testing.T) {
	resetPromptRepo(t)
	path := writeTestSkill(t, "ut-skill-cache", skillDocText("ut-cache", testSkillBody))

	first, firstReport := GetPromptRepository().LoadSkills([]string{"ut-skill-cache"})
	if len(first) != 1 {
		t.Fatalf("first load failed: %d", len(first))
	}
	if firstReport.Reloaded != 1 {
		t.Fatalf("first load should read from disk: %+v", firstReport)
	}

	swapped := strings.Replace(skillDocText("ut-cache", testSkillBody), "第一步", "第二步", 1)
	rewriteSameFingerprint(t, path, swapped)

	second, secondReport := GetPromptRepository().LoadSkills([]string{"ut-skill-cache"})
	if len(second) != 1 {
		t.Fatalf("second load failed: %d", len(second))
	}
	if secondReport.Cached != 1 || secondReport.Reloaded != 0 || len(secondReport.Skipped) != 0 {
		t.Fatalf("second load must be served from cache: %+v", secondReport)
	}
	if second[0].Content != first[0].Content {
		t.Fatalf("fingerprint unchanged should reuse cache (zero disk read): got=%q want=%q",
			second[0].Content, first[0].Content)
	}
	if !strings.Contains(second[0].Content, "第一步") {
		t.Fatalf("cached content should still be the old body: %q", second[0].Content)
	}
}

// TestLoadSkillsReloadsAfterContentChange 内容/指纹变化后必须重新读盘并重解析。
func TestLoadSkillsReloadsAfterContentChange(t *testing.T) {
	resetPromptRepo(t)
	path := writeTestSkill(t, "ut-skill-reload", skillDocText("ut-reload-v1", testSkillBody))

	if docs, _ := GetPromptRepository().LoadSkills([]string{"ut-skill-reload"}); len(docs) != 1 {
		t.Fatalf("first load failed: %d", len(docs))
	}

	changed := skillDocText("ut-reload-v2", "# 流程\n新正文\n")
	if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
		t.Fatalf("rewrite error:%s", err.Error())
	}

	docs, report := GetPromptRepository().LoadSkills([]string{"ut-skill-reload"})
	if len(docs) != 1 {
		t.Fatalf("reload failed: %d", len(docs))
	}
	if report.Reloaded != 1 || report.Cached != 0 {
		t.Errorf("content change must reload from disk: %+v", report)
	}
	if docs[0].FrontMatter.Name != "ut-reload-v2" {
		t.Errorf("front matter not reloaded: got=%q", docs[0].FrontMatter.Name)
	}
	if !strings.Contains(docs[0].Content, "新正文") {
		t.Errorf("content not reloaded: %q", docs[0].Content)
	}
}

// TestLoadSkillsSkipsMissingAndIllegal 缺失/非法入参只跳过,不报错也不返回半成品。
func TestLoadSkillsSkipsMissingAndIllegal(t *testing.T) {
	resetPromptRepo(t)

	docs, report := GetPromptRepository().LoadSkills([]string{"ut-skill-not-exist", "../escape", "   "})
	if len(docs) != 0 {
		t.Fatalf("no skill should be loaded: got=%d", len(docs))
	}
	if report.Requested != 3 || report.Loaded != 0 || len(report.Skipped) != 3 {
		t.Fatalf("every bad input should be reported as skipped: %+v", report)
	}
	// 合法目录名但文件不存在 ⇒ file not found(而不是静默丢弃)。
	// 注意:入参非法的技能先记录,读盘失败的技能后记录,不能假定下标。
	reasons := make([]string, 0, len(report.Skipped))
	for _, item := range report.Skipped {
		reasons = append(reasons, item.Reason)
	}
	if !strings.Contains(strings.Join(reasons, " | "), "not found") {
		t.Errorf("missing skill should report 'not found': %v", reasons)
	}
}

// TestLoadSkillsDeduplicates 同一技能的重复绑定(目录名 + 绝对路径)只保留一份。
func TestLoadSkillsDeduplicates(t *testing.T) {
	resetPromptRepo(t)
	path := writeTestSkill(t, "ut-skill-dup", skillDocText("ut-dup", testSkillBody))

	docs, report := GetPromptRepository().LoadSkills([]string{"ut-skill-dup", path})
	if len(docs) != 1 {
		t.Fatalf("duplicated binding should be deduped: got=%d want=1", len(docs))
	}
	if report.Duplicated != 1 {
		t.Errorf("duplicated binding should be counted: %+v", report)
	}

	// 重复绑定过的一对 SKILL.md(原来的实现会让工具名与目录项各出现两次)
	docs, report = GetPromptRepository().LoadSkills([]string{path, path})
	if len(docs) != 1 {
		t.Fatalf("duplicated absolute path should be deduped: got=%d want=1", len(docs))
	}
	if report.Duplicated != 1 || report.Cached != 1 {
		t.Errorf("second load of the same path should be a cache hit: %+v", report)
	}
}

// TestLoadSkillsKeepsBodyWhenFrontMatterBroken front matter 坏掉时仍保留正文(SOP 依赖正文)。
func TestLoadSkillsKeepsBodyWhenFrontMatterBroken(t *testing.T) {
	resetPromptRepo(t)
	broken := "---\nname: [unclosed\n---\n# 流程\n第三步:落盘\n"
	writeTestSkill(t, "ut-skill-badyaml", broken)

	docs, report := GetPromptRepository().LoadSkills([]string{"ut-skill-badyaml"})
	if len(docs) != 1 {
		t.Fatalf("broken front matter should not drop the skill: got=%d", len(docs))
	}
	if len(report.Skipped) != 0 {
		t.Errorf("broken front matter must not be reported as skipped: %+v", report)
	}
	if !strings.Contains(docs[0].Content, "第三步") {
		t.Errorf("body must be preserved: %q", docs[0].Content)
	}
	if docs[0].FrontMatter.Name != "" {
		t.Errorf("front matter should stay empty on parse error: %q", docs[0].FrontMatter.Name)
	}
}

// TestLoadSkillsSkipsOversizeDoc 超过 MaxSkillDocSize 的文档整体跳过(不截断、不报错)。
func TestLoadSkillsSkipsOversizeDoc(t *testing.T) {
	resetPromptRepo(t)
	writeTestSkill(t, "ut-skill-oversize", strings.Repeat("x", hCommon.MaxSkillDocSize+1))

	docs, report := GetPromptRepository().LoadSkills([]string{"ut-skill-oversize"})
	if len(docs) != 0 {
		t.Fatalf("oversize skill should be skipped: got=%d", len(docs))
	}
	if len(report.Skipped) != 1 || !strings.Contains(report.Skipped[0].Reason, "too large") {
		t.Fatalf("oversize skill should be reported with a reason: %+v", report)
	}
}

// TestDestroyDropsSkillCache Destroy 后必须重新读盘(缓存不得跨 Destroy 存活)。
func TestDestroyDropsSkillCache(t *testing.T) {
	resetPromptRepo(t)
	path := writeTestSkill(t, "ut-skill-destroy", skillDocText("ut-destroy", testSkillBody))

	first, _ := GetPromptRepository().LoadSkills([]string{"ut-skill-destroy"})
	if len(first) != 1 {
		t.Fatalf("first load failed: %d", len(first))
	}

	// 同尺寸改写 + 还原 mtime:缓存还在的话看不到新正文
	swapped := strings.Replace(skillDocText("ut-destroy", testSkillBody), "第一步", "第二步", 1)
	rewriteSameFingerprint(t, path, swapped)

	GetPromptRepository().Destroy()

	after, report := GetPromptRepository().LoadSkills([]string{"ut-skill-destroy"})
	if len(after) != 1 {
		t.Fatalf("load after destroy failed: %d", len(after))
	}
	if report.Reloaded != 1 || report.Cached != 0 {
		t.Errorf("cache must be dropped by Destroy: %+v", report)
	}
	if !strings.Contains(after[0].Content, "第二步") {
		t.Fatalf("cache should be dropped by Destroy: got=%q", after[0].Content)
	}
}

// TestLoadSkillsKeepsInputOrder 返回顺序 = 入参顺序(目录(名单)顺序决定工具与目录项的稳定性)。
func TestLoadSkillsKeepsInputOrder(t *testing.T) {
	resetPromptRepo(t)
	writeTestSkill(t, "ut-order-a", skillDocText("aaa", testSkillBody))
	writeTestSkill(t, "ut-order-b", skillDocText("bbb", testSkillBody))

	docs, _ := GetPromptRepository().LoadSkills([]string{"ut-order-b", "ut-order-a"})
	if len(docs) != 2 {
		t.Fatalf("skill count mismatch: got=%d want=2", len(docs))
	}
	if docs[0].SkillId != "ut-order-b" || docs[1].SkillId != "ut-order-a" {
		t.Errorf("order mismatch: got=[%s %s]", docs[0].SkillId, docs[1].SkillId)
	}
}

// TestSkillLoadReportLogSummary 两条分支(无跳过 / 有跳过)都必须能安全打日志:
// 这是“每轮零读盘”与“技能文件坏了”在线上唯一可见的两条信号,不能反过来自己炸掉。
func TestSkillLoadReportLogSummary(t *testing.T) {
	SkillLoadReport{Requested: 2, Loaded: 2, Cached: 2}.LogSummary("unit test: clean")

	SkillLoadReport{
		Requested: 2, Loaded: 1, Reloaded: 1,
		Skipped: []SkillSkipReason{{SkillFile: "/x/SKILL.md", Reason: "file not found"}},
	}.LogSummary("unit test: skipped")
}
