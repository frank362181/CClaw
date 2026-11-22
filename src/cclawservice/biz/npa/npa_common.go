package npa

import (
	"fmt"
	"regexp"
)

// 单字段保留的上限长度:超长字段进registry没有意义,还会撑爆日志
const maxFieldLength = 512

const (
	SpecFieldProjectDir = "project_dir" // 材料目录
	SpecFieldArchiveNo  = "archive_no"  // 档案号
	SpecFieldReportKind = "report_kind" // 报告类型
	SpecFieldOCR        = "ocr"         // OCR模式

	SpecDateLayout = "2006-01-02" // 日期格式

	DefaultReportKind = "不良资产尽职调查报告" //默认报告类型
	DefaultOCRMode    = "auto"       // OCR 默认模式

	MaterialsScanDepth = 2  // 材料扫描的深度
	MaterialScanLimit  = 32 // 材料扫描上限

	IntentionGuard = "本技能处理的事\"个贷不良资产材料->尽调报告\"" + "\n" + " - 用户只能在询问工具参数、流程说明或概念时，不要执行流程。\n" +
		" - 唯一必需的信息是**材料目录**;档案号由系统生成，不要询问、也不要编造。\n" + " - 材料目录里没有任何pdf/doc/docx/xlsx/jpg时，不要执行。\n" +
		" - 定位不到目录时,先用fs_list探测，仍不确定就追问用户，**不要用默认工作区**。\n\n"
)

// ToolArtifact 抽取出的产物不应包含SOP,避免cycle import
type ToolArtifact struct {
	Name   string
	TAKind string
	Ref    string
	Fields map[string]string
}

// 工具 -> 产物映射
type artifactSpec struct {
	Name    string
	TAKind  string
	RefKeys []string //依次尝试,取第一个非空
}

var archiveNoPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9\-_]{2,31}$`)

// DefaultOCRModes OCR的默认模式
var DefaultOCRModes = []string{"auto", "on", "off"}

// SpecIssue 规约的一条校验问题
type SpecIssue struct {
	Field  string `json:"field"`
	Detail string `json:"detail"`
}

func (s SpecIssue) String() string {
	return fmt.Sprintf("%s:%s", s.Field, s.Detail)
}
