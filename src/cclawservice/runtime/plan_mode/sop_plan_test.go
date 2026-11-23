package plan_mode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v2"

	"my.favorite/cclawservice/biz/npa"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/ctxmgr"
)

// npaOutline 与 bin/cclaw/skills/npa-report/SKILL.md 的 sop-out-lines 保持同构
// (3 章节版本:用例只关心结构,不必复制全部 7 节)。
func npaOutline() []hCommon.SkillSOPStep {
	return []hCommon.SkillSOPStep{
		{ID: "ingest", Kind: "tool", Tool: "npa_ingest", Produces: "parsed_context", Gate: "materials_manifest"},
		{ID: "ingest_ocr", Kind: "tool", Tool: "npa_ingest", When: "gate_failed(materials_manifest)",
			Produces: "parsed_context", Args: hCommon.SOPStepArgs{"path": "${parsed_context.ocr_dirs}"}},
		{ID: "extract", Kind: "tool", Tool: "npa_extract_metrics", Needs: []string{"parsed_context"},
			Produces: "extracted_metrics", Gate: "metrics_reconciled",
			Args: hCommon.SOPStepArgs{"input": "${parsed_context.path}"}},
		{ID: "compose", Kind: "llm", Role: "analyst", Needs: []string{"parsed_context", "extracted_metrics"},
			Produces: "draft", Sections: []string{"basic_info", "contracts", "conclusion"},
			Contract: &hCommon.SOPStepContract{Cite: "required", ForbidInvent: true}},
		{ID: "save", Kind: "tool", Tool: "npa_save_report", Needs: []string{"draft"}, Produces: "report_path",
			Args: hCommon.SOPStepArgs{
				"filename": "${spec.archive_no}_报告_${spec.today}.md",
				"content":  "${draft.text}",
			}},
		{ID: "verify", Kind: "check", Needs: []string{"draft"}, Produces: "verify_report", Checks: []string{"no_invention"}},
		{ID: "evaluate", Kind: "llm", Role: "evaluator", Needs: []string{"draft", "verify_report"}, MaxRounds: 2},
	}
}

func testArguments() PlanArguments {
	args := DefaultPlanArguments()
	args.KnownTools = map[string]struct{}{
		"npa_ingest": {}, "npa_extract_metrics": {}, "npa_save_report": {},
	}

	return args
}

func testSpec() npa.ReportSpec {
	return npa.ReportSpec{ProjectDir: "bin/workspace/10罗勇兴", ArchiveNo: "10罗勇兴", Today: "2026-11-13"}
}

// TestBuildPlanExpandsSectionsAndMerge 章节展开 + merge 生成 + 条件模板计数。
func TestBuildPlanExpandsSectionsAndMerge(t *testing.T) {
	plan, err := BuildPlanFromSkillSOPOutline("npa-report", npaOutline(), testSpec(), testArguments())
	if err != nil {
		t.Fatalf("展开失败: %v", err)
	}

	sections, merges := 0, 0

	for _, task := range plan.Tasks {
		if task.ParentID == "compose" && task.Kind == PlanTaskLLM {
			// 回归:章节任务必须有独立 ID,否则落库/UI/修复回路无法定位
			if want := "compose#" + task.Section; task.ID != want {
				t.Fatalf("章节任务 ID 应为 %q, got %q", want, task.ID)
			}

			sections++
		}

		if task.Kind == PlanTaskMerge {
			merges++

			if task.Produces != "draft" || len(task.Needs) != 3 {
				t.Fatalf("merge 任务错误: %+v", task)
			}
		}
	}

	if sections != 3 {
		t.Fatalf("章节任务数 want 3 got %d", sections)
	}
	if merges != 1 {
		t.Fatalf("merge 任务数 want 1 got %d", merges)
	}
	if plan.Conditional != 1 {
		t.Fatalf("条件模板数 want 1 got %d", plan.Conditional)
	}

	// 回归:预算必须真的被填(早期实现算了 tokens 却没有赋值)
	if plan.EstTokens <= 0 || plan.EstLatency <= 0 {
		t.Fatalf("预算未填充: tokens=%d latency=%s", plan.EstTokens, plan.EstLatency)
	}
}

// TestBuildPlanDepthAndWaves 深度链与波次(merge 必须在 save/verify 之前)。
func TestBuildPlanDepthAndWaves(t *testing.T) {
	plan, err := BuildPlanFromSkillSOPOutline("npa-report", npaOutline(), testSpec(), testArguments())
	if err != nil {
		t.Fatalf("展开失败: %v", err)
	}

	byID := make(map[string]PlanTask, len(plan.Tasks))
	for _, task := range plan.Tasks {
		byID[task.ID] = task
	}

	want := map[string]int{
		"ingest":             0,
		"extract":            1,
		"compose#basic_info": 2,
		"compose#contracts":  3,
		"compose#conclusion": 4,
		"compose_merge":      5,
		"save":               6,
		"verify":             6,
		"evaluate":           7,
	}

	for id, depth := range want {
		if byID[id].Depth != depth {
			t.Fatalf("%s depth want %d got %d", id, depth, byID[id].Depth)
		}
	}

	if plan.Waves != 8 {
		t.Fatalf("waves want 8 got %d", plan.Waves)
	}
}

// TestBuildPlanSequentialSectionsOff 并行模式:章节同层。
func TestBuildPlanSequentialSectionsOff(t *testing.T) {
	args := testArguments()
	args.SequentialSections = false

	plan, err := BuildPlanFromSkillSOPOutline("npa-report", npaOutline(), testSpec(), args)
	if err != nil {
		t.Fatalf("展开失败: %v", err)
	}

	for _, task := range plan.Tasks {
		if task.Section != "" && task.Depth != 2 {
			t.Fatalf("并行模式下章节应同层(depth=2), got %s depth=%d", task.ID, task.Depth)
		}
	}
}

// TestBuildPlanRendersSpecOnly 只渲染构建期已知的 ${spec.*},产物引用留给运行期。
func TestBuildPlanRendersSpecOnly(t *testing.T) {
	plan, err := BuildPlanFromSkillSOPOutline("npa-report", npaOutline(), testSpec(), testArguments())
	if err != nil {
		t.Fatalf("展开失败: %v", err)
	}

	for _, task := range plan.Tasks {
		if task.ID != "save" {
			continue
		}

		if got := task.Args["filename"]; got != "10罗勇兴_报告_2026-11-13.md" {
			t.Fatalf("${spec.*} 未渲染: %q", got)
		}
		if got := task.Args["content"]; got != "${draft.text}" {
			t.Fatalf("${artifact.*} 必须保留给运行期: %q", got)
		}

		return
	}

	t.Fatal("未找到 save 任务")
}

// TestBuildPlanRejectsUnknownArtifact 拼错产物名必须在构建期报错。
func TestBuildPlanRejectsUnknownArtifact(t *testing.T) {
	steps := append(npaOutline(), hCommon.SkillSOPStep{
		ID: "broken", Kind: "tool", Tool: "npa_save_report", Needs: []string{"draft"},
		Produces: "broken_path", Args: hCommon.SOPStepArgs{"content": "${typo.text}"},
	})

	_, err := BuildPlanFromSkillSOPOutline("npa-report", steps, testSpec(), testArguments())
	if err == nil || !strings.Contains(err.Error(), "plan invalid") {
		t.Fatalf("拼错产物名必须报错, got %v", err)
	}
}

// TestBuildPlanRejectsTooManyTasks 防病态计划(例如 sections 写了 200 个)。
func TestBuildPlanRejectsTooManyTasks(t *testing.T) {
	args := testArguments()
	args.MaxTasks = 3

	if _, err := BuildPlanFromSkillSOPOutline("npa-report", npaOutline(), testSpec(), args); err == nil {
		t.Fatal("超过 MaxTasks 必须报错")
	}
}

// TestBuildPlanRejectsInvalidOutline 结构不合法时不得产出计划。
func TestBuildPlanRejectsInvalidOutline(t *testing.T) {
	steps := []hCommon.SkillSOPStep{{ID: "write", Kind: "llm"}} // 没有终端产物

	if _, err := BuildPlanFromSkillSOPOutline("bad", steps, testSpec(), testArguments()); err == nil {
		t.Fatal("无效 SOP 必须报错")
	}
}

// TestNPAReportSkillSOPIsBuildable 防漂移:
// 直接解析仓库里真实的 SKILL.md,走与生产同一套 front-matter 解析,
// 断言 outline 合法且能展开成计划。文件缺失时跳过(CI 只跑部分目录)。
func TestNPAReportSkillSOPIsBuildable(t *testing.T) {
	path := filepath.Join("..", "..", "..", "bin", "cclaw", "skills", "npa-report", "SKILL.md")

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("SKILL.md 不可读,跳过: %v", err)
	}

	frontMatter, _ := ctxmgr.SplitFrontMatter(raw)

	var fm hCommon.SkillFrontMatter
	if err := yaml.Unmarshal([]byte(frontMatter), &fm); err != nil {
		t.Fatalf("front matter 解析失败: %v", err)
	}

	if !HasNpaOutline(fm) {
		t.Fatal("npa-report 必须声明 sop-out-lines")
	}

	if issues := ValidateNPAOutline(fm.SOPOutLines, OutlineToolNames(fm)); len(issues) > 0 {
		t.Fatalf("npa-report 的 sop-out-lines 不合规: %v", issues)
	}

	args := DefaultPlanArguments()
	args.KnownTools = OutlineToolNames(fm)

	plan, err := BuildPlanFromSkillSOPOutline("npa-report", fm.SOPOutLines, testSpec(), args)
	if err != nil {
		t.Fatalf("真实 SKILL.md 无法展开: %v", err)
	}

	if plan.Waves <= 0 || len(plan.Tasks) == 0 {
		t.Fatalf("计划异常: %+v", plan.PlanPayload)
	}

	// 7 章节 + 1 merge + (ingest/extract/save/verify 各 1) + evaluate
	t.Logf("真实 SKILL.md: tasks=%d waves=%d est_tokens=%d est_latency=%s",
		len(plan.Tasks), plan.Waves, plan.EstTokens, plan.EstLatency)
}

// BenchmarkBuildPlanFromSkillSOPOutline §14 M2′ DoD:纯函数 <1ms。
func BenchmarkBuildPlanFromSkillSOPOutline(b *testing.B) {
	steps := npaOutline()
	spec := testSpec()
	args := testArguments()

	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if _, err := BuildPlanFromSkillSOPOutline("npa-report", steps, spec, args); err != nil {
			b.Fatal(err)
		}
	}
}
