/**
* 不良资产最终报告所需的参数,并对其进行校验
**/

package npa

import (
	"fmt"
	"strings"

	myCommon "my.favorite/cclawservice/biz/common"
	pCommon "my.favorite/platformutility/common"
)

// ReportSpec 需求规约
type ReportSpec struct {
	ProjectDir string // 需求目录
	ArchiveNo  string // 需求档案号
	ReportKind string
	Today      string
	OCR        string
}

func (r ReportSpec) NormalizePaths(workRoot string) ReportSpec {
	raw := strings.TrimSpace(r.ProjectDir)
	if raw == pCommon.BlankString {
		return r
	}

	resolved, err := pCommon.ResolvePath(raw, workRoot)
	if err != nil || strings.TrimSpace(resolved) == pCommon.BlankString {
		return r
	}

	r.ProjectDir = resolved
	return r
}

// Fields 供模版渲染使用(键名即 ${spec.<key>}
func (r ReportSpec) Fields() map[string]string {
	return map[string]string{
		"project_dir": r.ProjectDir,
		"archive_no":  r.ArchiveNo,
		"report_kind": r.ReportKind,
		"today":       r.Today,
		"ocr":         r.OCR,
	}
}

func (r ReportSpec) WithField(key, value string) myCommon.ISpec {
	fields := r.Fields()
	if key == "" {
		return r
	}

	switch key {
	case "project_dir":
		r.ProjectDir = value
	case "archive_no":
		r.ArchiveNo = value
	case "report_kind":
		r.ReportKind = value
	case "ocr":
		r.OCR = value
	default:
		_ = fields
	}

	return r
}

func (r ReportSpec) Brief() string {
	archiveNo := strings.TrimSpace(r.ArchiveNo)
	if archiveNo == pCommon.BlankString {
		archiveNo = "(空)"
	}

	return fmt.Sprintf("目录=%s,档案号=%s,类型=%s,OCR=%s", r.ProjectDir, archiveNo, r.ReportKind, r.OCR)
}

var _ myCommon.ISpec = ReportSpec{}
