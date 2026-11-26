/**
* 业务侧协商参数定义
*
* 职责:
* - 参数 schema(来自于SKILL.md的schema断,数据驱动)
* - 参数解析(NegotiationArgument -> ReportSpec)
* - 判定说明
**/

package npa

import (
	"encoding/json"
	"fmt"
	"strings"

	myCommon "my.favorite/cclawservice/biz/common"
	hCommon "my.favorite/cclawservice/harness/common"
	pCommon "my.favorite/platformutility/common"
)

// negotiationArgument 协商产出的参数
// json tag 必须与 intention_start 工具 schema 的字段名一致(下划线),
// 否则 Unmarshal 时匹配不上,project_dir 会静默变成空串,
// 表现为"spec 校验失败:project_dir 未提供材料目录"
type negotiationArgument struct {
	ProjectDir string `json:"project_dir"`
	ReportKind string `json:"report_kind"`
	OCR        string `json:"ocr"`
}

type DdNegotiationSpec struct {
	fm                hCommon.SkillFrontMatter
	defaultProjectDir string // force模式下缺省材料目录
	workRoot          string
}

func NewNPANegotiationSpec(fm hCommon.SkillFrontMatter, projectDir string, workRoot string) *DdNegotiationSpec {
	return &DdNegotiationSpec{
		fm:                fm,
		defaultProjectDir: projectDir,
		workRoot:          workRoot,
	}
}

var _ myCommon.INegotiationSpec = (*DdNegotiationSpec)(nil)

func (sn *DdNegotiationSpec) ParamsSchema() map[string]any {
	properties := map[string]any{
		SpecFieldProjectDir: map[string]any{
			"type":        "string",
			"description": "材料目录绝对路径,必须已经存在",
		},
	}

	schema := parseSpecSchema(sn.fm.Schema)
	if len(schema.ReportKind) > 0 {
		properties[SpecFieldReportKind] = map[string]any{
			"type":        "string",
			"enum":        schema.ReportKind,
			"description": "报告类型,默认取第一个",
		}
	} else {
		properties[SpecFieldReportKind] = map[string]any{
			"type":        "string",
			"description": "报告类型",
		}
	}

	properties[SpecFieldOCR] = map[string]any{
		"type":        "string",
		"enum":        DefaultOCRModes,
		"description": "OCR模式,默认 auto",
	}

	return properties
}

func (sn *DdNegotiationSpec) ParamsRequired() []string {
	return []string{SpecFieldProjectDir}
}

func (sn *DdNegotiationSpec) Guard() string {
	return IntentionGuard
}

func (sn *DdNegotiationSpec) DecodeParams(raw json.RawMessage) (any, error) {
	var arg negotiationArgument
	if err := pCommon.UnmarshalJson(string(raw), &arg); err != nil {
		return nil, fmt.Errorf("unmarshal negotiationArgument: %w", err)
	}

	return ReportSpec{
		ProjectDir: strings.TrimSpace(arg.ProjectDir),
		ReportKind: arg.ReportKind,
		OCR:        arg.OCR,
	}, nil
}

func (sn *DdNegotiationSpec) ToSpec(raw any, fm hCommon.SkillFrontMatter) (myCommon.ISpec, bool) {
	decoded, ok := raw.(ReportSpec)
	if !ok {
		return nil, false
	}

	return NormalizeReportSpec(decoded.NormalizePaths(sn.workRoot), fm), true
}

func (sn *DdNegotiationSpec) Validate(spec myCommon.ISpec, fm hCommon.SkillFrontMatter) []string {
	reportSpec, ok := spec.(ReportSpec)
	if !ok {
		return []string{"内部错误:需求规约类型不匹配"}
	}

	issues := ValidateReportSpec(reportSpec, fm)
	if len(issues) == 0 {
		return nil
	}

	lines := make([]string, 0, len(issues))
	for _, issue := range issues {
		lines = append(lines, issue.String())
	}

	return lines
}

func (sn *DdNegotiationSpec) Default(fm hCommon.SkillFrontMatter) (myCommon.ISpec, []string) {
	candidate := NormalizeReportSpec(ReportSpec{
		ProjectDir: sn.defaultProjectDir,
		ReportKind: DefaultReportKind,
		OCR:        DefaultOCRMode,
	}.NormalizePaths(sn.workRoot), fm)

	issues := ValidateReportSpec(candidate, fm)
	if len(issues) == 0 {
		return candidate, nil
	}

	lines := make([]string, 0, len(issues))
	for _, issue := range issues {
		lines = append(lines, issue.String())
	}

	return candidate, lines
}

func (sn *DdNegotiationSpec) FormatIssues(issues []string) string {
	if len(issues) == 0 {
		return pCommon.BlankString
	}

	lines := make([]string, 0, len(issues)+2)
	lines = append(lines, "spec 校验失败:")
	for _, issue := range issues {
		lines = append(lines, " - "+issue)
	}

	lines = append(lines, "请用 fs_list 确认后重新表态;若用户尚未提供材料位置,改用need_more_info说明缺什么。")

	return strings.Join(lines, "\n")
}

func (sn *DdNegotiationSpec) FormatIssuesForUser(issues []string) string {
	if len(issues) == 0 {
		return pCommon.BlankString
	}

	lines := make([]string, 0, len(issues)+1)
	lines = append(lines, "需要补充以下信息才能开始:")
	for _, issue := range issues {
		lines = append(lines, issue)
	}

	return strings.Join(lines, "\n")
}

func (sn *DdNegotiationSpec) EnsureID(spec myCommon.ISpec, persisted map[string]string) myCommon.ISpec {
	reportSpec, ok := spec.(ReportSpec)
	if !ok {
		return spec
	}

	if strings.TrimSpace(reportSpec.ArchiveNo) != pCommon.BlankString {
		return reportSpec
	}

	if no := strings.TrimSpace(persisted[SpecFieldArchiveNo]); no != pCommon.BlankString {
		return reportSpec.WithField(SpecFieldArchiveNo, no)
	}

	return reportSpec.WithField(SpecFieldArchiveNo, GenerateDocumentNo(reportSpec.ProjectDir, "NPA"))
}

func (sn *DdNegotiationSpec) ReadPaths(spec myCommon.ISpec) []string {
	reportSpec, ok := spec.(ReportSpec)
	if !ok {
		return nil
	}

	if strings.TrimSpace(reportSpec.ProjectDir) == pCommon.BlankString {
		return nil
	}

	return []string{reportSpec.ProjectDir}
}
