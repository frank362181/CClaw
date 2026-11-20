package sop

import (
	"testing"

	hCommon "my.favorite/cclawservice/harness/common"
)

// 常量别名:让用例里只写 SOPStepKindTool 而不必每处都带 hCommon. 前缀。
// 真正的定义仍在 harness/common,这里只是测试内的简写。
const (
	SOPStepKindTool  = hCommon.SOPStepKindTool
	SOPStepKindLLM   = hCommon.SOPStepKindLLM
	SOPStepKindCheck = hCommon.SOPStepKindCheck
)

// step 构造一个 SOP 步骤,便于用例里只写关心的字段。
func step(id, kind string, mutate func(*hCommon.SkillSOPStep)) hCommon.SkillSOPStep {
	out := hCommon.SkillSOPStep{ID: id, Kind: kind}
	if mutate != nil {
		mutate(&out)
	}

	return out
}

func toolSet(names ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(names))
	for _, name := range names {
		out[name] = struct{}{}
	}

	return out
}

// validSteps 一条最小可用流水线:tool -> llm -> tool(交付) -> check。
func validSteps() []hCommon.SkillSOPStep {
	return []hCommon.SkillSOPStep{
		step("ingest", SOPStepKindTool, func(s *hCommon.SkillSOPStep) { s.Tool, s.Produces = "t_in", "raw" }),
		step("write", SOPStepKindLLM, func(s *hCommon.SkillSOPStep) { s.Needs, s.Produces = []string{"raw"}, "draft" }),
		step("save", SOPStepKindTool, func(s *hCommon.SkillSOPStep) {
			s.Tool, s.Needs, s.Produces = "t_save", []string{"draft"}, "report"
		}),
		step("verify", SOPStepKindCheck, func(s *hCommon.SkillSOPStep) {
			s.Needs, s.Checks, s.Produces = []string{"draft"}, []string{"no_invention"}, "verify_report"
		}),
	}
}

// TestValidateSOPOutlineOK 合法 SOP 必须零问题(否则正常技能会被误降级)。
func TestValidateSOPOutlineOK(t *testing.T) {
	if issues := ValidateNPAOutline(validSteps(), toolSet("t_in", "t_save")); len(issues) != 0 {
		t.Fatalf("合法 SOP 不应有问题: %v", issues)
	}
}

// TestValidateSOPOutlineRules 逐条锁定 L1–L8:
// 规则的价值在于"能拒绝什么",所以每条至少一个反例。
func TestValidateSOPOutlineRules(t *testing.T) {
	cases := []struct {
		name  string
		steps []hCommon.SkillSOPStep
		tools map[string]struct{}
		want  string // 期望命中的规则号;空表示"应当无问题"
	}{
		{
			name: "L1_id重复",
			steps: []hCommon.SkillSOPStep{
				step("dup", SOPStepKindTool, func(s *hCommon.SkillSOPStep) { s.Tool, s.Produces = "t_in", "a" }),
				step("dup", SOPStepKindLLM, func(s *hCommon.SkillSOPStep) { s.Produces = "b" }),
			},
			tools: toolSet("t_in"),
			want:  RuleDupID,
		},
		{
			name: "L1_kind非法",
			steps: []hCommon.SkillSOPStep{
				step("bad", "python", nil),
				step("ok", SOPStepKindLLM, func(s *hCommon.SkillSOPStep) { s.Produces = "b" }),
			},
			want: RuleDupID,
		},
		{
			name: "L2_悬空依赖",
			steps: []hCommon.SkillSOPStep{
				step("w", SOPStepKindLLM, func(s *hCommon.SkillSOPStep) { s.Needs, s.Produces = []string{"nope"}, "d" }),
			},
			want: RuleDanglingNeed,
		},
		{
			name: "L2_依赖声明在后",
			steps: []hCommon.SkillSOPStep{
				step("w", SOPStepKindLLM, func(s *hCommon.SkillSOPStep) { s.Needs, s.Produces = []string{"raw"}, "d" }),
				step("i", SOPStepKindTool, func(s *hCommon.SkillSOPStep) { s.Tool, s.Produces = "t_in", "raw" }),
			},
			tools: toolSet("t_in"),
			want:  RuleDanglingNeed,
		},
		{
			name: "L3_未注册工具",
			steps: []hCommon.SkillSOPStep{
				step("i", SOPStepKindTool, func(s *hCommon.SkillSOPStep) { s.Tool, s.Produces = "unknown_tool", "raw" }),
				step("w", SOPStepKindLLM, nil),
			},
			tools: toolSet("t_in"),
			want:  RuleUnknownTool,
		},
		{
			name: "L4_tool缺produces",
			steps: []hCommon.SkillSOPStep{
				step("i", SOPStepKindTool, func(s *hCommon.SkillSOPStep) { s.Tool = "t_in" }),
				step("w", SOPStepKindLLM, func(s *hCommon.SkillSOPStep) { s.Produces = "d" }),
			},
			tools: toolSet("t_in"),
			want:  RuleMissingField,
		},
		{
			name: "L4_check缺checks",
			steps: []hCommon.SkillSOPStep{
				step("w", SOPStepKindLLM, func(s *hCommon.SkillSOPStep) { s.Produces = "d" }),
				step("v", SOPStepKindCheck, func(s *hCommon.SkillSOPStep) { s.Needs, s.Produces = []string{"d"}, "vr" }),
			},
			want: RuleMissingField,
		},
		{
			name: "L5_没有llm",
			steps: []hCommon.SkillSOPStep{
				step("i", SOPStepKindTool, func(s *hCommon.SkillSOPStep) { s.Tool, s.Produces = "t_in", "raw" }),
			},
			tools: toolSet("t_in"),
			want:  RuleNoLLMStep,
		},
		{
			// 回归:早期实现用覆盖式赋值判断 hasLlm,末步不是 llm 时会误报"缺少 llm"。
			name: "L5_末步为check不得误报",
			steps: []hCommon.SkillSOPStep{
				step("w", SOPStepKindLLM, func(s *hCommon.SkillSOPStep) { s.Produces = "d" }),
				step("v", SOPStepKindCheck, func(s *hCommon.SkillSOPStep) {
					s.Needs, s.Checks, s.Produces = []string{"d"}, []string{"c"}, "vr"
				}),
			},
		},
		{
			// 回归:只写不落盘必须被拦(SOP 的"没落盘就不算完成")。
			name: "L6_没有终端产物",
			steps: []hCommon.SkillSOPStep{
				step("a", SOPStepKindTool, func(s *hCommon.SkillSOPStep) { s.Tool, s.Produces = "t_in", "x" }),
				step("b", SOPStepKindLLM, func(s *hCommon.SkillSOPStep) { s.Needs, s.Produces = []string{"x"}, "y" }),
				step("c", SOPStepKindLLM, func(s *hCommon.SkillSOPStep) { s.Needs = []string{"y"} }),
			},
			tools: toolSet("t_in"),
			want:  RuleNoDeliverable,
		},
		{
			name: "L7_章节未禁编造",
			steps: []hCommon.SkillSOPStep{
				step("w", SOPStepKindLLM, func(s *hCommon.SkillSOPStep) {
					s.Produces, s.Sections = "d", []string{"a", "b"}
				}),
			},
			want: RuleContractAbsent,
		},
		{
			name: "L8_args指定评审者",
			steps: []hCommon.SkillSOPStep{
				step("w", SOPStepKindLLM, func(s *hCommon.SkillSOPStep) {
					s.Produces = "d"
					s.Args = hCommon.SOPStepArgs{"reviewer_agent": "agent_002"}
				}),
			},
			want: RuleReviewerField,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues := ValidateNPAOutline(tc.steps, tc.tools)

			if tc.want == "" {
				if len(issues) != 0 {
					t.Fatalf("不应有问题: %v", issues)
				}

				return
			}

			for _, issue := range issues {
				if issue.Rule == tc.want {
					return
				}
			}

			t.Fatalf("缺少规则 %s, got=%v", tc.want, issues)
		})
	}
}

// TestValidateSOPOutlineEmpty outline 为空时也要给出明确问题(便于加载期打日志)。
func TestValidateSOPOutlineEmpty(t *testing.T) {
	issues := ValidateNPAOutline(nil, nil)
	if len(issues) == 0 {
		t.Fatal("空 outline 必须返回问题")
	}
}

// TestSOPOutlineToolNames 工具名集合必须与 tools[] 完全一致(L3 的唯一依据)。
func TestSOPOutlineToolNames(t *testing.T) {
	fm := hCommon.SkillFrontMatter{
		Tools: []hCommon.BasicTool{
			{Name: " npa_ingest "},
			{Name: "npa_save_report"},
			{Name: ""},
			{Name: "   "},
		},
	}

	names := OutlineToolNames(fm)
	if len(names) != 2 {
		t.Fatalf("应过滤空名, got=%v", names)
	}
	for _, want := range []string{"npa_ingest", "npa_save_report"} {
		if _, ok := names[want]; !ok {
			t.Fatalf("缺少工具 %q, got=%v", want, names)
		}
	}

	if !HasNpaOutline(hCommon.SkillFrontMatter{SOPOutLines: validSteps()}) {
		t.Fatal("声明了 sop-out-lines 时 HasSOP 必须为 true")
	}
	if HasNpaOutline(hCommon.SkillFrontMatter{}) {
		t.Fatal("未声明时 HasSOP 必须为 false")
	}
}
