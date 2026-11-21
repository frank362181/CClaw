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
	fm hCommon.SkillFrontMatter
}

func NewNPANegotiationSpec(fm hCommon.SkillFrontMatter) *DdNegotiationSpec {
	return &DdNegotiationSpec{
		fm: fm,
	}
}

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
