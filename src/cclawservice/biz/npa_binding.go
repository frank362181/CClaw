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
	"my.favorite/cclawservice/chat/sop"
)

// RegistryNPABuiltin 将NPA业务注册进harness
func RegistryNPABuiltin(extractors *sop.Extractors, barriers *sop.BarrierRepository) {
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
func npaExtract(planTask sop.PlanTask, raw string) (sop.Artifact, error) {
	extracted, err := npa.ExtractToolArtifact(planTask.Action, raw)
	if err != nil {
		return sop.Artifact{}, err
	}

	return sop.Artifact{
		Name:         extracted.Name,
		ArtifactKind: extracted.TAKind,
		AbsolutePath: extracted.Ref,
		Fields:       extracted.Fields,
	}, nil
}

func npaBarrie(barrie func(fields map[string]string) []npa.Finding) sop.Barrie {
	return func(in sop.TaskBarrierInput) []sop.Gap {
		findings := barrie(in.Output.Fields)
		if len(findings) == 0 {
			return nil
		}

		gaps := make([]sop.Gap, 0, len(findings))
		for _, finding := range findings {
			severity := sop.SeverityWarning
			if finding.Fatal {
				severity = sop.SeverityFail
			}

			gaps = append(gaps, sop.Gap{
				Code:     strings.TrimSpace(finding.Code),
				TaskID:   in.Task.ID,
				Section:  in.Task.Section,
				Detail:   sop.StatusBlocked,
				Severity: severity,
			})
		}

		return gaps
	}
}
