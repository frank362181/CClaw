/**
* 启动文件/SKILL.md/系统工具 schema —— 统一"仓库读盘 + 缓存派生"机制的测试。
*
* 覆盖三层:
*   1. readDocFile:四态判定(缺失/目录/符号链接/超限/正常)与指纹同构;
*   2. 缓存层:指纹命中零读盘、文件变化即失效、超大/非法整份跳过、重复入参去重;
*   3. 消费层:三态注入语义、front matter 派生、系统工具 Visible 以 DB 为准。
**/

package ctxmgr

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
)

// ============================ 测试夹具 ============================

func utWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// utSkillDir 在技能根下建一个唯一技能目录,返回目录名与 SKILL.md 绝对路径。
func utSkillDir(t *testing.T, name, body string) (string, string) {
	t.Helper()
	root, _ := BuildSkillRoot()
	docPath := filepath.Join(root, name, SkillDocFileName)
	utWrite(t, docPath, body)
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Join(root, name)) })
	return name, docPath
}

// utSystemToolDir 在系统工具根下建一个唯一工具目录,返回目录名、schema 与脚本路径。
func utSystemToolDir(t *testing.T, name string, schemaJSON string) (string, string, string) {
	t.Helper()
	root := filepath.Join(pCommon.GetExecutableDir(), hCommon.AgentBootstrapDir, hCommon.SystemToolDirRoot)
	dir := filepath.Join(root, name)
	schemaPath := filepath.Join(dir, "schema.json")
	scriptPath := filepath.Join(dir, "scripts", "run.py")

	utWrite(t, schemaPath, schemaJSON)
	utWrite(t, scriptPath, "# probe script\n")
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	return name, schemaPath, scriptPath
}

// ============================ 1. readDocFile ============================

// TestReadDocFileStates 锁定统一读盘的四态判定与指纹语义。
func TestReadDocFileStates(t *testing.T) {
	t.Run("正常文件", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "ok.md")
		utWrite(t, path, "hello\n")

		snap := ReadDocFile(path, 1024)
		if snap.State != FileReady {
			t.Fatalf("State=%v reason=%q", snap.State, snap.Reason)
		}
		if snap.Content != "hello\n" || snap.ByteSize != int64(len("hello\n")) {
			t.Errorf("Content/ByteSize 不符: %q %d", snap.Content, snap.ByteSize)
		}
		if snap.Fingerprint == "" {
			t.Error("FileReady 必须带指纹")
		}
		if snap.SkipReason() != "file not found" {
			t.Errorf("FileReady 无 Reason 时 SkipReason 应回落缺省: %q", snap.SkipReason())
		}
	})

	t.Run("文件缺失", func(t *testing.T) {
		snap := ReadDocFile(filepath.Join(t.TempDir(), "nope.md"), 1024)
		if snap.State != FileMissing {
			t.Fatalf("State=%v want FileMissing", snap.State)
		}
		if snap.Content != "" || snap.ByteSize != 0 {
			t.Error("缺失不应带内容/大小")
		}
	})

	t.Run("目录按缺失(不报不可用)", func(t *testing.T) {
		snap := ReadDocFile(t.TempDir(), 1024)
		if snap.State != FileMissing {
			t.Fatalf("State=%v want FileMissing", snap.State)
		}
	})

	t.Run("符号链接按缺失", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "real.md")
		link := filepath.Join(root, "link.md")
		utWrite(t, target, "inner\n")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlink unsupported: %v", err)
		}

		snap := ReadDocFile(link, 1024)
		if snap.State != FileMissing {
			t.Fatalf("State=%v want FileMissing(O_NOFOLLOW)", snap.State)
		}
		if strings.Contains(snap.Content, "inner") {
			t.Error("不得跟随符号链接读到目标内容")
		}
	})

	t.Run("超限按不可用", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "big.md")
		utWrite(t, path, strings.Repeat("x", 2048))

		snap := ReadDocFile(path, 1024)
		if snap.State != FileUnavailable {
			t.Fatalf("State=%v want FileUnavailable", snap.State)
		}
		if !strings.Contains(snap.Reason, "too large") {
			t.Errorf("Reason 应说明超限: %q", snap.Reason)
		}
		if snap.ByteSize != 2048 {
			t.Errorf("不可用时应回带真实大小供诊断, got=%d", snap.ByteSize)
		}
	})
}

// TestReadDocFileFingerprintMatchesPathStat 锁定缓存命中的前提:
// 缓存判定用 ComputeFileFingerprint(path),读盘存的是打开句柄算出的指纹 —— 二者必须同构,
// 否则缓存永不命中(每轮重读+重解析)。
func TestReadDocFileFingerprintMatchesPathStat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fp.md")
	utWrite(t, path, "fp-check\n")

	byPath := pCommon.ComputeFileFingerprint(path)
	byHandle := ReadDocFile(path, 1024).Fingerprint

	if byPath == "" || byHandle == "" {
		t.Fatalf("指纹不得为空: path=%q handle=%q", byPath, byHandle)
	}
	if byPath != byHandle {
		t.Fatalf("指纹不同构 -> 缓存永不命中: path=%q handle=%q", byPath, byHandle)
	}
}

// ============================ 2. LoadSkills 缓存 ============================

// TestLoadSkillsCacheHitAndInvalidate 锁定技能缓存的三条核心行为:
// 首次读盘解析、指纹未变零读盘、文件变化立即失效并拿到新内容。
func TestLoadSkillsCacheHitAndInvalidate(t *testing.T) {
	body := "---\nname: ut-skill\ndescription: 单元测试技能\ntrigger-keywords:\n  - 单元测试\n---\n## SOP\n1. 步骤一\n"
	name, docPath := utSkillDir(t, "ut-skill-cache", body)

	dc := GetAgentDocCache()

	first, r1 := dc.LoadSkills([]string{name})
	if len(first) != 1 || r1.Reloaded != 1 || r1.Cached != 0 {
		t.Fatalf("首次应读盘派生: len=%d report=%+v", len(first), r1)
	}
	doc := first[0]
	if doc.SkillId != name {
		t.Errorf("SkillId 应取目录名: got=%q want=%q", doc.SkillId, name)
	}
	if doc.SkillFile != docPath {
		t.Errorf("SkillFile 不符: got=%q want=%q", doc.SkillFile, docPath)
	}
	if doc.FrontMatter.Name != "ut-skill" || !strings.Contains(doc.FrontMatter.Description, "单元测试技能") {
		t.Errorf("front matter 未解析: %+v", doc.FrontMatter)
	}
	if len(doc.FrontMatter.TriggerKeywords) != 1 || doc.FrontMatter.TriggerKeywords[0] != "单元测试" {
		t.Errorf("trigger-keywords 未解析: %+v", doc.FrontMatter.TriggerKeywords)
	}
	if strings.Contains(doc.Content, "name: ut-skill") || !strings.Contains(doc.Content, "步骤一") {
		t.Errorf("正文应剥离 front matter: %q", doc.Content)
	}
	if doc.ByteSize != int64(len(body)) {
		t.Errorf("ByteSize 应为原始字节数: got=%d want=%d", doc.ByteSize, len(body))
	}

	// 第二次:指纹未变 -> 命中缓存,零读盘零解析
	second, r2 := dc.LoadSkills([]string{name})
	if len(second) != 1 || r2.Cached != 1 || r2.Reloaded != 0 {
		t.Fatalf("第二次应命中缓存: report=%+v", r2)
	}
	if second[0].Content != doc.Content {
		t.Error("命中缓存的内容应与首次一致")
	}

	// 内容变化(size 与 mtime 都变)-> 指纹失效,必须重新读盘
	newBody := "---\nname: ut-skill\ndescription: 已更新\n---\n## SOP\n1. 步骤二(更长的新内容)\n"
	utWrite(t, docPath, newBody)

	third, r3 := dc.LoadSkills([]string{name})
	if len(third) != 1 || r3.Reloaded != 1 {
		t.Fatalf("文件变化后应重新读盘: report=%+v", r3)
	}
	if !strings.Contains(third[0].Content, "步骤二") {
		t.Errorf("必须拿到新内容而不是旧缓存: %q", third[0].Content)
	}
	if third[0].Fingerprint == doc.Fingerprint {
		t.Error("文件变化后指纹必须变化")
	}
}

// TestLoadSkillsSkipAndDedup 锁定"非法/不可读"一律整份跳过、重复入参去重。
func TestLoadSkillsSkipAndDedup(t *testing.T) {
	name, docPath := utSkillDir(t, "ut-skill-skip", "---\nname: ut-skip\n---\n正文\n")
	dc := GetAgentDocCache()

	// 重复绑定:同一技能传两次只保留一份
	out, report := dc.LoadSkills([]string{name, name, ""})
	if len(out) != 1 {
		t.Fatalf("重复入参应去重: len=%d", len(out))
	}
	if report.Duplicated != 1 {
		t.Errorf("应记录 1 条重复: %+v", report)
	}
	if len(report.Skipped) != 1 {
		t.Errorf("空技能目录应记入 Skipped: %+v", report.Skipped)
	}

	// 目录不存在 -> 跳过,不 panic、不返回半成品
	out, report = dc.LoadSkills([]string{"ut-skill-not-exist"})
	if len(out) != 0 || len(report.Skipped) != 1 {
		t.Fatalf("缺失技能应跳过: len=%d report=%+v", len(out), report)
	}

	// 越界目录 -> 被路径校验拦下
	out, report = dc.LoadSkills([]string{"../etc"})
	if len(out) != 0 || len(report.Skipped) != 1 {
		t.Fatalf("越界技能目录应被拦截: len=%d report=%+v", len(out), report)
	}

	// 超限 SKILL.md -> 整份跳过(不截断注入半份 SOP)
	utWrite(t, docPath, strings.Repeat("甲", MaxSkillDocSize+1))
	out, report = dc.LoadSkills([]string{name})
	if len(out) != 0 || len(report.Skipped) != 1 {
		t.Fatalf("超限技能应整份跳过: len=%d report=%+v", len(out), report)
	}

	// 恢复后应能重新加载(证明跳过没有污染缓存)
	utWrite(t, docPath, "---\nname: ut-skip\n---\n恢复的正文\n")
	out, report = dc.LoadSkills([]string{name})
	if len(out) != 1 || !strings.Contains(out[0].Content, "恢复的正文") {
		t.Fatalf("恢复后应能加载: report=%+v", report)
	}
}

// ============================ 3. LoadSystemTools 缓存 ============================

const utToolSchema = `{
  "name": "uttool",
  "description": "单元测试用系统工具",
  "input_schema": {
    "type": "object",
    "properties": {"path": {"type": "string"}},
    "required": ["path"]
  }
}`

func utTool(toolName, schemaFile, script string, visible bool) dataobject.TTool {
	return dataobject.TTool{
		ToolName:   toolName,
		SchemaFile: schemaFile,
		Script:     script,
		Visible:    visible,
	}
}

// TestLoadSystemToolsParseAndCache 锁定系统工具 schema 的解析产物与缓存命中。
func TestLoadSystemToolsParseAndCache(t *testing.T) {
	name, _, scriptPath := utSystemToolDir(t, "uttool", utToolSchema)
	dc := GetAgentDocCache()

	first := dc.LoadSystemTools([]dataobject.TTool{utTool(name, "schema.json", "scripts/run.py", true)})
	if first.Loaded != 1 || first.Reloaded != 1 {
		t.Fatalf("首次应解析: %+v", first)
	}

	docs, err := dc.GetSystemToolSchemaDocument(nil, nil)
	if err != nil {
		t.Fatalf("GetSystemToolSchemaDocument: %v", err)
	}
	var got *hCommon.SystemToolSchemaDocument
	for i := range docs {
		if docs[i].ToolName == "uttool" {
			got = &docs[i]
		}
	}
	if got == nil {
		t.Fatalf("缓存中应能取到 uttool: %+v", docs)
	}
	if got.Script != scriptPath {
		t.Errorf("脚本路径应为绝对路径: got=%q want=%q", got.Script, scriptPath)
	}
	if got.SysToolSchemaMeta.Description == "" || len(got.SysToolSchemaMeta.InputSchema) == 0 {
		t.Errorf("schema 元数据丢失(模型将无法正确调用): %+v", got.SysToolSchemaMeta)
	}
	if got.SchemaFingerprint == "" {
		t.Error("应记录 schema 指纹用于缓存判定")
	}

	// 第二次:schema 未变 -> 命中缓存
	second := dc.LoadSystemTools([]dataobject.TTool{utTool(name, "schema.json", "scripts/run.py", true)})
	if second.Cached != 1 || second.Reloaded != 0 {
		t.Fatalf("schema 未变应命中缓存: %+v", second)
	}
}

// TestLoadSystemToolsVisibleFollowsDB 锁定关键语义:
// schema 文件未变时命中缓存(不重读盘),但 Visible 必须每次都以**数据库最新配置**为准 ——
// 否则把 bash 设为不可见后,服务不重启就永远拦不住。
//
// 契约:GetSystemToolSchemaDocument 只返回 Visible=true 的工具(直接剔除,而非带回标记)。
func TestLoadSystemToolsVisibleFollowsDB(t *testing.T) {
	name, _, _ := utSystemToolDir(t, "uttool2", strings.Replace(utToolSchema, "uttool", "uttool2", 1))
	dc := GetAgentDocCache()

	dc.LoadSystemTools([]dataobject.TTool{utTool(name, "schema.json", "scripts/run.py", true)})

	// inVisibleSet 报告该工具是否出现在"下发给模型"的可见集合中
	inVisibleSet := func() bool {
		docs, _ := dc.GetSystemToolSchemaDocument(nil, nil)
		for i := range docs {
			if docs[i].ToolName == name {
				return true
			}
		}
		return false
	}
	// cachedVisible 直接读缓存条目的 Visible 标记(同包可见)
	cachedVisible := func() (bool, bool) {
		dc.mu.RLock()
		defer dc.mu.RUnlock()
		doc, hit := dc.systemToolSchemaCache[name]
		return doc.Visible, hit
	}

	if !inVisibleSet() {
		t.Fatal("初始应可见")
	}

	// schema 文件不动,只翻转 DB 的 visible
	report := dc.LoadSystemTools([]dataobject.TTool{utTool(name, "schema.json", "scripts/run.py", false)})
	if report.Cached != 1 {
		t.Fatalf("schema 未变应命中缓存(不重读盘): %+v", report)
	}
	if inVisibleSet() {
		t.Error("Visible=false 后必须从可见集合中剔除")
	}
	if visible, hit := cachedVisible(); !hit || visible {
		t.Errorf("缓存条目的 Visible 必须跟随 DB 刷新为 false: hit=%v visible=%v", hit, visible)
	}

	// 再翻回可见:schema 依旧未变,应重新进入可见集合
	report = dc.LoadSystemTools([]dataobject.TTool{utTool(name, "schema.json", "scripts/run.py", true)})
	if report.Cached != 1 || !inVisibleSet() {
		t.Fatalf("翻回可见后应命中缓存并重新纳入: %+v", report)
	}
}

// TestLoadSystemToolsSkipsInvalid 锁定非法 schema 一律跳过而非注入半成品。
func TestLoadSystemToolsSkipsInvalid(t *testing.T) {
	cases := []struct {
		name   string
		schema string
		reason string
	}{
		{"utbad-json", `{"name": "utbad-json", `, "JSON 截断"},
		{"utbad-noname", `{"description": "no name", "input_schema": {"type":"object"}}`, "缺 name"},
		{"utbad-noparams", `{"name": "utbad-noparams", "description": "d"}`, "缺 input_schema"},
	}

	dc := GetAgentDocCache()
	for _, c := range cases {
		name, _, _ := utSystemToolDir(t, c.name, c.schema)
		report := dc.LoadSystemTools([]dataobject.TTool{utTool(name, "schema.json", "scripts/run.py", true)})
		if report.Loaded != 0 || len(report.Skipped) != 1 {
			t.Errorf("%s(%s) 应被跳过: %+v", c.name, c.reason, report)
		}
	}

	// schema 文件缺失同样跳过
	report := dc.LoadSystemTools([]dataobject.TTool{utTool("utmissing", "schema.json", "scripts/run.py", true)})
	if report.Loaded != 0 || len(report.Skipped) != 1 {
		t.Errorf("schema 缺失应跳过: %+v", report)
	}

	// 空工具名 / 重复名
	report = dc.LoadSystemTools([]dataobject.TTool{utTool("  ", "schema.json", "s.py", true)})
	if len(report.Skipped) != 1 {
		t.Errorf("空工具名应跳过: %+v", report)
	}

	name, _, _ := utSystemToolDir(t, "uttool3", strings.Replace(utToolSchema, "uttool", "uttool3", 1))
	report = dc.LoadSystemTools([]dataobject.TTool{
		utTool(name, "schema.json", "scripts/run.py", true),
		utTool(name, "schema.json", "scripts/run.py", true),
	})
	if report.Duplicated != 1 || report.Loaded != 1 {
		t.Errorf("同名工具应去重: %+v", report)
	}
}

// TestLoadSystemToolsCacheKeyFollowsDBName 锁定缓存键的同源性:
// 回写键必须与查找键一致(都用归一化的 DB tool_name)。
// 若 DB 名与 schema 内部的 name 不一致、而回写仍用 schema name,
// 缓存将永不命中 —— 每轮重读+重解析,且没有任何日志提示。
func TestLoadSystemToolsCacheKeyFollowsDBName(t *testing.T) {
	// DB 名 utkey-tool,而 schema 内的 name 故意取一个不相等的值
	name, _, _ := utSystemToolDir(t, "utkey-tool", strings.Replace(utToolSchema, "uttool", "UtKey-Different", 1))
	dc := GetAgentDocCache()
	tool := dataobject.TTool{ToolName: name, SchemaFile: "schema.json", Script: "scripts/run.py", Visible: true}

	first := dc.LoadSystemTools([]dataobject.TTool{tool})
	if first.Loaded != 1 || first.Reloaded != 1 || first.Cached != 0 {
		t.Fatalf("首次应读盘解析: %+v", first)
	}

	second := dc.LoadSystemTools([]dataobject.TTool{tool})
	if second.Loaded != 1 || second.Cached != 1 || second.Reloaded != 0 {
		t.Fatalf("DB 名与 schema name 不一致时缓存仍必须命中(回写键须与查找键同源): %+v", second)
	}

	// 名称不一致不得影响正确性:schema 元数据仍须完整
	docs, _ := dc.GetSystemToolSchemaDocument(nil, nil)
	var found bool
	for i := range docs {
		if docs[i].ToolName == "UtKey-Different" {
			found = true
			if docs[i].SysToolSchemaMeta.Description == "" || len(docs[i].SysToolSchemaMeta.InputSchema) == 0 {
				t.Errorf("名称不一致不得影响 schema 元数据完整性: %+v", docs[i].SysToolSchemaMeta)
			}
		}
	}
	if !found {
		t.Error("名称不一致的工具仍应可被取出(以 schema name 对外)")
	}
}

// ============================ 3.1 统一读盘的边界与语义 ============================

// TestReadDocFileSizeBoundary 锁定统一读盘的大小边界:恰好等于上限可用,超出即不可用。
func TestReadDocFileSizeBoundary(t *testing.T) {
	root := t.TempDir()

	exact := filepath.Join(root, "exact.md")
	utWrite(t, exact, strings.Repeat("x", 1024))
	if snap := ReadDocFile(exact, 1024); snap.State != FileReady {
		t.Errorf("恰好等于上限应可用: State=%v reason=%q", snap.State, snap.Reason)
	}

	over := filepath.Join(root, "over.md")
	utWrite(t, over, strings.Repeat("x", 1025))
	if snap := ReadDocFile(over, 1024); snap.State != FileUnavailable {
		t.Errorf("超出上限应不可用: State=%v", snap.State)
	}
}

// TestStartupFileOversizeStateAndReason 锁定超限启动文件的两个语义:
//   - 状态必须是 FileUnavailable(而不是被折成"缺失");
//   - Reason 必须明确指向超限,不能落到 SkipReason 的缺省 "file not found"
//     (否则日志会把"文件太大"误报成"文件不存在",误导排查)。
func TestStartupFileOversizeStateAndReason(t *testing.T) {
	root := t.TempDir()
	utWrite(t, filepath.Join(root, hCommon.AgentDocFileName), strings.Repeat("x", hCommon.MaxAgentDocSize+1))

	snap, err := GetAgentFileRepository().Snapshot(root)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	var item *StartupFileSnapshotItem
	for i := range snap.Files {
		if snap.Files[i].Spec.Name == hCommon.AgentDocFileName {
			item = &snap.Files[i]
		}
	}
	if item == nil {
		t.Fatal("AGENTS.md 应出现在快照中")
	}
	if item.State != FileUnavailable {
		t.Fatalf("State=%v want FileUnavailable", item.State)
	}
	if !strings.Contains(item.Reason, "too large") {
		t.Errorf("Reason 应指向超限,而不是缺省文案: %q", item.Reason)
	}
	if item.ByteSize != hCommon.MaxAgentDocSize+1 {
		t.Errorf("应回带真实大小供诊断: got=%d", item.ByteSize)
	}

	// 就绪项的 Reason 必须为空(Reason 只在非就绪时才有意义,避免误导日志)
	root2 := t.TempDir()
	utWrite(t, filepath.Join(root2, hCommon.AgentDocFileName), "ok\n")
	snap2, _ := GetAgentFileRepository().Snapshot(root2)
	for i := range snap2.Files {
		if snap2.Files[i].Spec.Name == hCommon.AgentDocFileName && snap2.Files[i].Reason != "" {
			t.Errorf("就绪项不应携带 Reason: %q", snap2.Files[i].Reason)
		}
	}
}

// TestOversizeBoundaryConsistentAcrossReadPaths 锁定"统一读盘"的边界一致:
// 单文件路径(GetStartupFile)与批量快照(Snapshot)对同一文件必须得出同一结论。
// 两者若各留一份读盘实现,边界(上限±1)最容易漂移。
func TestOversizeBoundaryConsistentAcrossReadPaths(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, hCommon.AgentDocFileName)
	repo := GetAgentFileRepository()

	state := func() AgentFileState {
		snap, err := repo.Snapshot(root)
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		for i := range snap.Files {
			if snap.Files[i].Spec.Name == hCommon.AgentDocFileName {
				return snap.Files[i].State
			}
		}
		t.Fatal("快照缺少 AGENTS.md")
		return FileMissing
	}

	// 恰好等于上限:两条路径都必须接受
	utWrite(t, file, strings.Repeat("x", hCommon.MaxAgentDocSize))
	if got := state(); got != FileReady {
		t.Errorf("恰好等于上限: 快照应 FileReady, got=%v", got)
	}
	if doc, err := repo.GetStartupFile(root, hCommon.AgentDocFileName); err != nil || doc.Missing {
		t.Errorf("恰好等于上限: 单文件读取应成功, doc=%+v err=%v", doc, err)
	}

	// 上限 +1:两条路径都必须判为不可用(快照 FileUnavailable / 单文件报错)
	utWrite(t, file, strings.Repeat("x", hCommon.MaxAgentDocSize+1))
	if got := state(); got != FileUnavailable {
		t.Errorf("超出上限: 快照应 FileUnavailable, got=%v", got)
	}
	if _, err := repo.GetStartupFile(root, hCommon.AgentDocFileName); err == nil {
		t.Error("超出上限: 单文件读取应报错")
	}
}

// TestGetSystemToolSchemaDocumentFiltersDisallowed 锁定 disallowed-tools 的过滤语义。
func TestGetSystemToolSchemaDocumentFiltersDisallowed(t *testing.T) {
	dc := GetAgentDocCache()
	nameA, _, _ := utSystemToolDir(t, "utfilter-a", strings.Replace(utToolSchema, "uttool", "utfilter-a", 1))
	nameB, _, _ := utSystemToolDir(t, "utfilter-b", strings.Replace(utToolSchema, "uttool", "utfilter-b", 1))
	dc.LoadSystemTools([]dataobject.TTool{
		utTool(nameA, "schema.json", "scripts/run.py", true),
		utTool(nameB, "schema.json", "scripts/run.py", true),
	})

	names := func(list []hCommon.SystemToolSchemaDocument) []string {
		out := make([]string, 0, len(list))
		for i := range list {
			out = append(out, list[i].ToolName)
		}
		return out
	}

	docs, _ := dc.GetSystemToolSchemaDocument(nil, nil)
	if !contains(names(docs), nameA) || !contains(names(docs), nameB) {
		t.Fatalf("默认应都可见: %v", names(docs))
	}

	docs, _ = dc.GetSystemToolSchemaDocument(nil, []string{nameA})
	got := names(docs)
	if contains(got, nameA) {
		t.Errorf("disallowed 的工具必须被剔除: %v", got)
	}
	if !contains(got, nameB) {
		t.Errorf("未被禁的工具应保留: %v", got)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// ============================ 4. LoadAgentBootstrap 三态注入 ============================

func utAgent(t *testing.T, agentId, dirName string) dataobject.TAgent {
	t.Helper()
	root := filepath.Join(pCommon.GetExecutableDir(), hCommon.AgentBootstrapDir, "utowner", dirName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	return dataobject.TAgent{AgentId: agentId, OwnerId: "utowner", AgentDir: dirName}
}

// TestLoadAgentBootstrapThreeStates 锁定启动文档的注入语义与可复用性判定。
func TestLoadAgentBootstrapThreeStates(t *testing.T) {
	agent := utAgent(t, "ut-agent-states", "states")
	root := filepath.Join(pCommon.GetExecutableDir(), hCommon.AgentBootstrapDir, "utowner", "states")
	dc := GetAgentDocCache()

	// AGENTS.md(必需,正常) + MEMORY.md(可选,超限)
	utWrite(t, filepath.Join(root, hCommon.AgentDocFileName),
		"---\nsummary: 摘要A\n---\nAGENTS-BODY\n")
	utWrite(t, filepath.Join(root, hCommon.MemoryDocFileName),
		strings.Repeat("m", hCommon.MaxAgentDocSize+1))

	fp1, err := dc.LoadAgentBootstrap(agent)
	if err != nil {
		t.Fatalf("LoadAgentBootstrap: %v", err)
	}
	if fp1 == "" {
		t.Fatal("指纹不应为空")
	}

	byName := map[string]hCommon.AgentDocument{}
	for _, doc := range dc.GetAgentDocuments(agent.AgentId) {
		byName[doc.FileName] = doc
	}

	// 正常文件:注入正文并剥离 front matter
	agents, ok := byName[hCommon.AgentDocFileName]
	if !ok || agents.Missing {
		t.Fatalf("AGENTS.md 应正常注入: ok=%v %+v", ok, agents)
	}
	if agents.Content != "AGENTS-BODY\n" {
		t.Errorf("front matter 未剥离: %q", agents.Content)
	}
	if agents.FrontMatter.Summary != "摘要A" {
		t.Errorf("front matter 未解析: %q", agents.FrontMatter.Summary)
	}

	// 存在但超限(可选):告警省略,不得注入占位
	if doc, ok := byName[hCommon.MemoryDocFileName]; ok {
		t.Errorf("超限文件应被省略,却得到 Missing=%v 的占位", doc.Missing)
	}

	// 缺失的必需文件(AGENTS.md 是白名单中唯一的必需项):保留占位,供组装层注入 [MISSING]
	emptyAgent := utAgent(t, "ut-agent-empty", "empty")
	if _, err := dc.LoadAgentBootstrap(emptyAgent); err != nil {
		t.Fatalf("加载空工作区: %v", err)
	}
	docs := dc.GetAgentDocuments(emptyAgent.AgentId)
	if len(docs) == 0 {
		t.Fatal("缺失的必需文件必须保留占位,不能整个工作区为空")
	}
	for _, doc := range docs {
		if !doc.Required {
			t.Errorf("只有必需文件才该保留占位,却出现可选文件: %s", doc.FileName)
		}
		if !doc.Missing {
			t.Errorf("占位必须是 Missing=true: %+v", doc)
		}
	}
	if docs[0].FileName != hCommon.AgentDocFileName {
		t.Errorf("占位应是 AGENTS.md: %s", docs[0].FileName)
	}

	// 文件未变 -> 复用(指纹相同)
	fp2, _ := dc.LoadAgentBootstrap(agent)
	if fp2 != fp1 {
		t.Errorf("文件未变时指纹应稳定: %q -> %q", fp1, fp2)
	}

	// 文件变化 -> 指纹变化 + 文档更新
	utWrite(t, filepath.Join(root, hCommon.AgentDocFileName), "---\nsummary: 摘要B\n---\nAGENTS-BODY-V2\n")
	fp3, _ := dc.LoadAgentBootstrap(agent)
	if fp3 == fp1 {
		t.Error("文件变化后指纹必须变化")
	}
	for _, doc := range dc.GetAgentDocuments(agent.AgentId) {
		if doc.FileName == hCommon.AgentDocFileName {
			if !strings.Contains(doc.Content, "V2") || doc.FrontMatter.Summary != "摘要B" {
				t.Errorf("必须拿到更新后的文档: %q / %q", doc.Content, doc.FrontMatter.Summary)
			}
		}
	}
}

// TestLoadAgentBootstrapRejectsUnsafeDir 锁定工作区路径安全校验。
func TestLoadAgentBootstrapRejectsUnsafeDir(t *testing.T) {
	dc := GetAgentDocCache()
	for _, dir := range []string{"../escape", "", "/abs/path"} {
		agent := dataobject.TAgent{AgentId: "ut-agent-unsafe", OwnerId: "utowner", AgentDir: dir}
		if _, err := dc.LoadAgentBootstrap(agent); err == nil {
			t.Errorf("非法 AgentDir=%q 应被拒绝", dir)
		}
	}
}

// ============================ 5. 并发 ============================

// TestUnifiedCacheConcurrent 并发跑三层缓存,配合 -race 验证无数据竞争。
func TestUnifiedCacheConcurrent(t *testing.T) {
	skillName, _ := utSkillDir(t, "ut-skill-concurrent", "---\nname: ut-conc\n---\n并发正文\n")
	toolName, _, _ := utSystemToolDir(t, "utconc", strings.Replace(utToolSchema, "uttool", "utconc", 1))
	agent := utAgent(t, "ut-agent-concurrent", "conc")

	dc := GetAgentDocCache()
	tools := []dataobject.TTool{utTool(toolName, "schema.json", "scripts/run.py", true)}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(4)
		go func() { defer wg.Done(); dc.LoadSkills([]string{skillName}) }()
		go func() { defer wg.Done(); dc.LoadSystemTools(tools) }()
		go func() { defer wg.Done(); _, _ = dc.LoadAgentBootstrap(agent) }()
		go func() {
			defer wg.Done()
			_, _ = dc.GetSystemToolSchemaDocument(nil, nil)
			_ = dc.GetAgentDocuments(agent.AgentId)
		}()
	}
	wg.Wait()
}
