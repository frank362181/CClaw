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

	IntentionGuard = " - 材料位置的两种写法：①材料目录绝对路径；②债务人姓名(如 罗勇兴、10罗勇兴)。\n" +
		" - 只给姓名时：用 glob 查材料，参数 path=<材料根目录>、pattern=\"*<姓名>*/**\"；\n" +
		"   命中一个目录就用它的绝对路径填 project_dir；命中多个就 need_more_info 让用户选；\n" +
		"   一个都没有就追问准确姓名或要绝对路径。\n"
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
