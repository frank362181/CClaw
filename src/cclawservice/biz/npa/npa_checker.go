/**
* 从SKILL.md定义的内容中将NPA 业务相关的检查器抽取出来,并对结果进行检查
*
* 设计意图:
* - 与SKILL.md 一一对应:materials_check != true 就是fail
* - files_need_ocr 非空只能 warn(触发ingest_ocr条件任务),不是失败--这正是"边做边扩计划"
* - 不依赖于文件系统,只看数字/布尔字段 -> 纯函数
**/

package npa

import (
	"fmt"
	"strconv"
	"strings"

	cCommon "my.favorite/cclawservice/common"
	pCommon "my.favorite/platformutility/common"
)

const (
	CheckerNameMaterialsManifest = "materials_manifest"
	CheckerNameMetricsReconciled = "metrics_reconciled"
	CheckNameSaveOK              = "save_ok"
)

// Finding 一条任务完成后的检查结论
type Finding struct {
	Code   string
	Detail string
	Fatal  bool // true -> 阻断后续步骤;false -> 仅提示
}

// MaterialsManifestChecker 获取材料的检查器
// - 有需要OCR的文件 -> 提示(由条件任务 ingest_ocr 补读),不阻断
// - 清单份数与实际解析份数不一致 -- 阻断
func MaterialsManifestChecker(fields map[string]string) []Finding {
	findings := make([]Finding, 0, 2)

	if needOCR := strings.TrimSpace(fields["summary.files_need_ocr"]); needOCR != "" {
		findings = append(findings, Finding{
			Code:   "ocr_required",
			Detail: "OCR required" + pCommon.TruncateRunes(needOCR, 200, cCommon.TruncatedMarker),
		})
	}

	parsed, okParsed := intField(fields, "summary.files_parsed")
	total, okTotal := intField(fields, "summary.files")
	if okParsed && okTotal && parsed != total {
		findings = append(findings, Finding{
			Code:   "materials_mismatch",
			Detail: fmt.Sprintf("materials unmatched:parsed %d / %d", parsed, total),
			Fatal:  true,
		})
	}

	return findings
}

// MetricsReconciledChecker 指标检查器
// - materials_check.match必须为true
// - 文档数必须等于材料的份数
// - 去重折叠数 > 0 / 1 有 warning -> 提示
func MetricsReconciledChecker(fields map[string]string) []Finding {
	findings := make([]Finding, 0, 3)

	if !boolField(fields, "summary.materials_check.match") {
		findings = append(findings, Finding{
			Code:   "materials_check_failed",
			Detail: "materials unmatched,can not continue",
			Fatal:  true,
		})
	}

	docs, okDocs := intField(fields, "summary.documents_unique")
	total, okTotal := intField(fields, "summary.files")
	if okDocs && okTotal && docs != total {
		findings = append(findings, Finding{
			Code:   "documents_mismatch",
			Detail: fmt.Sprintf("documents unmatched:documents_unique %d / %d", docs, total),
			Fatal:  true,
		})
	}

	if collapsed, ok := intField(fields, "summary.duplicates_collapsed"); ok && collapsed > 0 {
		findings = append(findings, Finding{
			Code:   "duplicates_collapsed",
			Detail: fmt.Sprintf("%d documents collapsed,please check", collapsed),
		})
	}

	if count, ok := intField(fields, "summary.warnings.count"); ok && count > 0 {
		findings = append(findings, Finding{
			Code:   "extract_warning",
			Detail: fmt.Sprintf("%d warnings whiling extracting", count),
		})
	}

	return findings
}

// SaveOKChecker 落盘的写入检查器,必须要有路径
func SaveOKChecker(fields map[string]string) []Finding {
	ref := strings.TrimSpace(fields["path"])
	if ref == "" {
		ref = strings.TrimSpace(fields["result_file.path"])
	}

	if ref == "" {
		return []Finding{{
			Code:   "save_failed",
			Detail: "save failed,non file path",
			Fatal:  true,
		}}
	}

	return nil
}

func intField(fields map[string]string, key string) (int, bool) {
	text, ok := fields[key]
	if !ok {
		return 0, false
	}

	value, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return 0, false
	}

	return value, true
}

func boolField(fields map[string]string, key string) bool {
	return strings.EqualFold(strings.TrimSpace(fields[key]), "true")
}
