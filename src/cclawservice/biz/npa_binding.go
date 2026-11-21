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

	"my.favorite/cclawservice/biz/npa"
	"my.favorite/cclawservice/chat/plan_mode"
	pCommon "my.favorite/platformutility/common"
)

// RegistryNPABuiltin 将NPA业务注册进harness
func RegistryNPABuiltin(extractors *plan_mode.Extractors, barriers *plan_mode.BarrierRepository) {
	if extractors == nil || barriers == nil {
		return
	}

	extractors.RegisterExtractor(npa.ToolNameIngest, npaExtract)
	extractors.RegisterExtractor(npa.ToolNameExtractMetrics, npaExtract)
	extractors.RegisterExtractor(npa.ToolNameSaveOK, npaExtract)

	barriers.Registry(npa.CheckerNameMaterialsManifest, npaBarrie(npa.MaterialsManifestChecker))
	barriers.Registry(npa.CheckerNameMetricsReconciled, npaBarrie(npa.MetricsReconciledChecker))
	barriers.Registry(npa.CheckNameSaveOK, npaBarrie(npa.SaveOKChecker))
}

// 工具输出 -> Artifact
func npaExtract(planTask plan_mode.PlanTask, raw string) (plan_mode.Artifact, error) {
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

func npaBarrie(barrie func(fields map[string]string) []npa.Finding) plan_mode.Barrie {
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
				Detail:   plan_mode.StatusBlocked,
				Severity: severity,
			})
		}

		return gaps
	}
}
