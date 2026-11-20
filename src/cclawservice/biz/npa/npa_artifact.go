/**
* 目的:
*	将工具JSON输出压成扁平字段(map[string]string),后续任务执行结果的检查都只读它
*
* 设计意图:
*	- 依赖方向:npa 不认识 SOP,返回自己的ToolArtifact
*	- 容错优先:脚本输出字段可能随版本变化,抽取器缺字段不报错,只留空 -> 任务执行后的检查器降级为warn/fail;
*	- 限制体积:单个字段值 > 512 字符直接丢弃(避免把整篇parsed_context塞进registry日志)
*	- 只抽白名单工具:未登记的工具不产出产物(防"随便一个工具都算产物")
**/

package npa

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	pCommon "my.favorite/platformutility/common"
)

const (
	// ToolNameIngest 与SKILL.md中的函数名绑定
	ToolNameIngest         = "npa_ingest"
	ToolNameExtractMetrics = "npa_extract_metrics"
	ToolNameSaveOK         = "npa_save_report"

	ArtifactParsedContext  = "parsed_context"
	ArtifactExtractMetrics = "extract_metrics"
	ArtifactReportPath     = "report_path"
)

var artifactSpecs = map[string]artifactSpec{
	ToolNameIngest: {
		Name:    ArtifactParsedContext,
		TAKind:  "file",
		RefKeys: []string{"result_file.path", "parsed_file", "result_file"},
	},

	ToolNameExtractMetrics: {
		Name:    ArtifactExtractMetrics,
		TAKind:  "file",
		RefKeys: []string{"result_file.path", "extracted_file", "result_file"},
	},

	ToolNameSaveOK: {
		Name:    ArtifactReportPath,
		TAKind:  "file",
		RefKeys: []string{"path", "result_file.path"},
	},
}

// IsArtifactTool 该工具是否产出产物
func IsArtifactTool(toolName string) bool {
	_, ok := artifactSpecs[strings.ToLower(toolName)]
	return ok
}

// ExtractToolArtifact 解析工具输出 JSON -> 产物
func ExtractToolArtifact(toolName, raw string) (ToolArtifact, error) {
	spec, ok := artifactSpecs[strings.ToLower(toolName)]
	if !ok {
		return ToolArtifact{}, fmt.Errorf("artifact extract: tool %s un registry outcome", toolName)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ToolArtifact{}, fmt.Errorf("artifact extract:outcome not JSON object:%w", err)
	}

	// 脚本失败时返回错误:不算产物,交给调用方按失败处理
	if msg, hit := payload["error"]; hit {
		return ToolArtifact{}, fmt.Errorf("artifact extract:error happened:%v", msg)
	}

	fields := make(map[string]string, 16)
	pCommon.FlattenJSON(pCommon.BlankString, payload, maxFieldLength, fields)

	// 派生字段:需要OCR的父亲目录
	fields["ocr_dirs"] = ocrDirs(fields["summary.files_need_ocr"])
	artifact := ToolArtifact{
		Name:   spec.Name,
		TAKind: spec.TAKind,
		Fields: fields,
	}

	for _, key := range spec.RefKeys {
		if ref := strings.TrimSpace(fields[key]); ref != pCommon.BlankString {
			artifact.Ref = ref
			break
		}
	}

	if artifact.Ref == pCommon.BlankString {
		return ToolArtifact{}, fmt.Errorf("artifact extract: outcome path %s not found(%v)", toolName, spec.RefKeys)
	}

	return artifact, nil
}

func ocrDirs(filesNeedOCR string) string {
	if strings.TrimSpace(filesNeedOCR) == pCommon.BlankString {
		return pCommon.BlankString
	}

	seen := make(map[string]struct{}, 4)
	for _, file := range strings.Split(filesNeedOCR, ",") {
		if file == pCommon.BlankString {
			continue
		}

		dir := path.Dir(strings.ReplaceAll(file, "\\", "/"))
		if dir == "." || dir == "/" || dir == pCommon.BlankString {
			continue
		}

		seen[dir] = struct{}{}
	}

	dirs := make([]string, 0, len(seen))
	for dir := range seen {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return strings.Join(dirs, ",")
}
