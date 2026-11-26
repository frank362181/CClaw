/**
* 设计目的:
*	把与业务相关的数据和业务接入到SOP的Artifact/Gap上,业务方之需要调用RegisterBizBuilt即可
*
* 设计意图:
* - 只有一个适配点:将来新的行业SKILl,依葫芦画瓢写一个xxx_binding.go,编写签名与RegistryNPABuiltin一样的函数即可;
* - 任务完成后的检查器由门禁task.Gate决定,绑定表"门径名 -> npa纯函数"映射,避免运行反向映射
*
**/

package biz

import (
	"strings"

	myCommon "my.favorite/cclawservice/biz/common"
	"my.favorite/cclawservice/biz/npa"
	"my.favorite/cclawservice/config"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/runtime/plan_mode"
	pCommon "my.favorite/platformutility/common"
)

// TypeName 定义业务的名称
type TypeName string

const (
	TypeNameNPA TypeName = "npa-report"
)

func GetNegotiationSpec(skillId string, fm hCommon.SkillFrontMatter, chattingConfig config.ChatConfig, workRoot string) myCommon.INegotiationSpec {
	switch strings.ToLower(strings.TrimSpace(skillId)) {
	case string(TypeNameNPA):
		return npa.NewNPANegotiationSpec(fm, chattingConfig.Negotiation.ProjectDir, workRoot)
	default:
		return nil
	}
}

// RegistryNPABuiltin 将NPA业务注册进harness
func RegistryNPABuiltin(extractors *plan_mode.Extractors, barriers *plan_mode.BarrierRepository) {
	if extractors == nil || barriers == nil {
		return
	}

	extractors.RegisterExtractor(npa.ToolNameIngest, getExtractSchema)
	extractors.RegisterExtractor(npa.ToolNameExtractMetrics, getExtractSchema)
	extractors.RegisterExtractor(npa.ToolNameSaveOK, getExtractSchema)

	barriers.Registry(npa.CheckerNameMaterialsManifest, getBarrieSchema(npa.MaterialsManifestChecker))
	barriers.Registry(npa.CheckerNameMetricsReconciled, getBarrieSchema(npa.MetricsReconciledChecker))
	barriers.Registry(npa.CheckNameSaveOK, getBarrieSchema(npa.SaveOKChecker))
}

// 工具输出 -> Artifact
func getExtractSchema(planTask plan_mode.PlanTask, raw string) (plan_mode.Artifact, error) {
	extracted, err := npa.ExtractToolArtifact(planTask.Action, raw)
	if err != nil {
		return plan_mode.Artifact{}, err
	}

	name := planTask.Produces
	if name == pCommon.BlankString {
		name = extracted.Name
	}

	return plan_mode.Artifact{
		Name:         name,
		ArtifactKind: extracted.TAKind,
		AbsolutePath: extracted.Ref,
		Fields:       extracted.Fields,
	}, nil
}

func getBarrieSchema(barrie func(fields map[string]string) []npa.Finding) plan_mode.Barrie {
	return func(in plan_mode.TaskBarrierInput) []plan_mode.Gap {
		findings := barrie(in.Output.Fields)
		if len(findings) == 0 {
			return nil
		}

		gaps := make([]plan_mode.Gap, 0, len(findings))
		for _, finding := range findings {
			severity := plan_mode.SeverityWarning
			if finding.Fatal {
				severity = plan_mode.SeverityFail
			}

			gaps = append(gaps, plan_mode.Gap{
				Code:     strings.TrimSpace(finding.Code),
				TaskID:   in.Task.ID,
				Section:  in.Task.Section,
				Detail:   finding.Detail,
				Severity: severity,
			})
		}

		return gaps
	}
}
