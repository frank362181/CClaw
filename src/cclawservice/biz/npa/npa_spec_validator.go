/**
* 需求规约的校验与归一化
*
* 职责:只回答"参数能不能用"
*	- 机制校验(绝对路径/存在/可读/非空)实现在这里
*	- 业务判断(什么是材料、支持哪些报告类型、档案号格式)来自SKILL.md
* 	- “该不该跑”不再这里判断--由模型 reason + plan预览 + 可选审批负责
**/

package npa

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	hCommon "my.favorite/cclawservice/harness/common"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"
)

type SpecSchema struct {
	Materials  []string `json:"materials" yaml:"materials"`   // 受认可的材料类型
	ReportKind []string `json:"reportKind" yaml:"reportKind"` // 受认可的报告类型
}

func NormalizeReportSpec(spec ReportSpec, fm hCommon.SkillFrontMatter) ReportSpec {
	normalizedSpec := ReportSpec{
		ProjectDir: strings.TrimSpace(spec.ProjectDir),
		ArchiveNo:  strings.ToUpper(strings.TrimSpace(spec.ArchiveNo)),
		ReportKind: strings.TrimSpace(spec.ReportKind),
		OCR:        strings.ToLower(strings.TrimSpace(spec.OCR)),
		Today:      time.Now().Format(SpecDateLayout),
	}

	skillSchema := parseSpecSchema(fm.Schema)
	if normalizedSpec.ReportKind == pCommon.BlankString {
		if len(skillSchema.ReportKind) > 0 {
			normalizedSpec.ReportKind = skillSchema.ReportKind[0]
		} else {
			normalizedSpec.ReportKind = DefaultReportKind
		}
	}

	if normalizedSpec.OCR == pCommon.BlankString {
		normalizedSpec.OCR = DefaultOCRMode
	}

	return normalizedSpec
}

func ValidateReportSpec(spec ReportSpec, fm hCommon.SkillFrontMatter) []SpecIssue {
	skillSchema := parseSpecSchema(fm.Schema)

	issues := make([]SpecIssue, 0, 4)
	issues = append(issues, validateProjectDir(spec.ProjectDir, skillSchema)...)
	issues = append(issues, validateReportKind(spec.ReportKind, skillSchema)...)
	issues = append(issues, validateArchiveNo(spec.ArchiveNo, skillSchema)...)
	issues = append(issues, validateOCR(spec.OCR)...)

	return issues
}

// WalkMaterial 供测试/调试使用
func WalkMaterial(dir string, materials []string, limit int) []string {
	found := make([]string, 0, limit)
	if limit <= 0 {
		limit = 8
	}

	_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(entry.Name()))
		for _, m := range materials {
			if ext == strings.ToLower(strings.TrimSpace(m)) {
				found = append(found, path)
				if len(found) >= limit {
					return filepath.SkipAll
				}

				break
			}
		}
		return nil
	})

	return found
}

func parseSpecSchema(raw any) SpecSchema {
	blocks := specSchemaBlocks(raw)

	if len(blocks) == 0 {
		return SpecSchema{}
	}

	return SpecSchema{
		Materials:  specSchemaStringList(blocks["materials"]), // materials字段来自SKILL.md
		ReportKind: specSchemaStringList(blocks["report_kind"]),
	}
}

func specSchemaBlocks(raw any) map[string]any {
	switch typed := raw.(type) {
	case nil:
		return nil
	case map[string]any:
		return typed
	case map[interface{}]interface{}:
		blocks := make(map[string]any, len(typed))
		for k, v := range typed {
			text, ok := k.(string)
			if !ok {
				continue
			}
			blocks[text] = v
		}
		return blocks
	default:
		logging.LogWarnF("npa: unsupported type %T, ignored", raw)
		return nil
	}
}

func specSchemaStringList(raw any) []string {
	switch typed := raw.(type) {
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, v := range typed {
			if text, ok := v.(string); ok {
				if trimmed := strings.TrimSpace(text); len(trimmed) > 0 {
					out = append(out, strings.ToLower(trimmed))
				}
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(typed))
		for _, v := range typed {
			if trimmed := strings.TrimSpace(v); len(trimmed) > 0 {
				out = append(out, strings.ToLower(trimmed))
			}
		}
		return out
	case string:
		if trimmed := strings.TrimSpace(typed); len(trimmed) > 0 {
			return []string{strings.ToLower(trimmed)}
		}
		return nil
	default:
		return nil
	}
}

func specSchemaBool(raw any) bool {
	switch typed := raw.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func validateProjectDir(dir string, skillSchema SpecSchema) []SpecIssue {
	//-----机制校验:与业务无关
	if dir == pCommon.BlankString {
		return []SpecIssue{{Field: SpecFieldProjectDir, Detail: "未提供材料目录"}}
	}

	if !filepath.IsAbs(dir) {
		return []SpecIssue{{Field: SpecFieldProjectDir, Detail: "必须是绝对路径:" + dir}}
	}

	info, err := os.Stat(dir)
	if err != nil {
		return []SpecIssue{{Field: SpecFieldProjectDir, Detail: "路径不存在:" + dir}}
	}

	if !info.IsDir() {
		return []SpecIssue{{Field: SpecFieldProjectDir, Detail: "不是目录:" + dir}}
	}

	if _, err := os.ReadDir(dir); err != nil {
		return []SpecIssue{{Field: SpecFieldProjectDir, Detail: "目录不可读:" + dir}}
	}

	// ----业务判断:扩展名白名单
	if len(skillSchema.Materials) == 0 {
		// 技能没有声明材料类型时,只要求目录非空,不替它发明规则
		if entries, _ := os.ReadDir(dir); len(entries) == 0 {
			return []SpecIssue{{Field: SpecFieldProjectDir, Detail: "目录为空:" + dir}}
		}

		return nil
	}

	scanned := scanDirForMaterial(dir, skillSchema.Materials, 0, MaterialsScanDepth)
	if !scanned {
		return []SpecIssue{{
			Field:  SpecFieldProjectDir,
			Detail: fmt.Sprintf("目录内（含 %d 层子目录）未发现 %s 材料", MaterialsScanDepth, strings.Join(skillSchema.Materials, "/")),
		}}
	}

	return nil
}

func validateArchiveNo(archiveNo string, _ SpecSchema) []SpecIssue {
	if strings.TrimSpace(archiveNo) == pCommon.BlankString {
		return nil
	}

	if !archiveNoPattern.MatchString(archiveNo) {
		return []SpecIssue{{Field: SpecFieldArchiveNo, Detail: "档案号格式不符合要求:" + archiveNo}}
	}

	return nil
}

func validateReportKind(reportKind string, skillSchema SpecSchema) []SpecIssue {
	if len(skillSchema.ReportKind) == 0 {
		return nil
	}

	for _, kind := range skillSchema.ReportKind {
		if strings.EqualFold(strings.TrimSpace(kind), reportKind) {
			return nil
		}
	}

	return []SpecIssue{{Field: SpecFieldReportKind,
		Detail: fmt.Sprintf("不支持的报告类型[%s],可选[%s]", reportKind, strings.Join(skillSchema.ReportKind, "/"))}}
}

func validateOCR(ocr string) []SpecIssue {
	for _, mode := range DefaultOCRModes {
		if strings.EqualFold(ocr, mode) {
			return nil
		}
	}

	return []SpecIssue{{Field: SpecFieldOCR, Detail: fmt.Sprintf("不支持的 OCR模式[%s],可选 [%s]", ocr, strings.Join(DefaultOCRModes, "/"))}}
}

// 扫描目录&子目录是否有指定的材料
func scanDirForMaterial(dir string, materials []string, depth, maxDepth int) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}

	scanned := 0
	for _, entry := range entries {
		if scanned >= MaterialScanLimit {
			break
		}
		scanned++

		if entry.IsDir() {
			if depth >= maxDepth {
				continue
			}

			if scanDirForMaterial(filepath.Join(dir, entry.Name()), materials, depth, maxDepth) {
				return true
			}
			continue
		}

		ext := strings.ToLower(filepath.Ext(entry.Name()))
		for _, m := range materials {
			if ext == strings.ToLower(strings.TrimSpace(m)) {
				return true
			}
		}
	}

	return false
}
