/**
* 意图识别的三个内置工具
*
* 设计原因:
* - 协商阶段只允许只读操作,不能把执行业务的工具暴露给LLM
* - 只读探测需要稳定、0副作用、0启动成本,Go实现较为合适
* - 路径策略与ToolConfigRules同源
**/

package chat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"my.favorite/cclawservice/common/llmproto"
	"my.favorite/cclawservice/harness/toolexecutor"
	pCommon "my.favorite/platformutility/common"
)

const (
	IntentionToolFsList = "fs_list"         // 只读目录
	IntentionToolFsStat = "fs_stat"         // 只探测路径
	IntentionToolStart  = "intention_start" // 表态并提交参数

	maxListEntries = 200 // 目录列举上限
)

// =============================== schema ===============================

func getProbeToolSchema() []llmproto.Tool {
	return []llmproto.Tool{
		{
			Type: "function",
			Function: llmproto.ToolFunction{
				Name:        IntentionToolFsList,
				Description: "列出目录下的文件名、大小与扩展名统计。只读，不返回文件内容。用于确认材料目录是否正确、里面有哪些材料。",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":      map[string]any{"type": "string", "description": "要列出的绝对路径（目录）"},
						"recursive": map[string]any{"type": "boolean", "description": "是否递归一层子目录，默认 false"},
					},
					"required": []string{"path"},
				},
			},
		},
		{
			Type: "function",
			Function: llmproto.ToolFunction{
				Name:        IntentionToolFsStat,
				Description: "",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{"type": "string", "description": "绝对路径"},
					},
					"required": []string{"path"},
				},
			},
		},
	}
}

func getIntentionStartToolSchema(skillId string, spec INegotiationSpec) llmproto.Tool {
	properties := map[string]any{
		"decision": map[string]any{
			"type":        "string",
			"enum":        []string{"proceed", "need_more_info", "not_applicable"},
			"description": "proceed=执行流程;need_more_info=信息不足需要追问用户;not_applicable=用户只是在询问用法,不需要执行流程",
		},
		"reason": map[string]any{
			"type":        "string",
			"description": "为什么这么判断,一句话。必填。",
		},
		"missing": map[string]any{
			"type":        "string",
			"items":       map[string]any{"type": "string"},
			"description": "decision=need_more_info 时必填:缺哪些信息",
		},
	}

	if params, required := negotiateParams(spec); params != nil {
		specObject := map[string]any{
			"type":       "object",
			"properties": params,
		}

		if len(required) > 0 {
			specObject["required"] = required
		}

		properties["spec"] = specObject
	}
	return llmproto.Tool{
		Type: "function",
		Function: llmproto.ToolFunction{
			Name: IntentionToolStart,
			Description: fmt.Sprintf("表态工具:说明是否执行 %s 的 SOP 流程,并在执行时给出参数。"+
				"不要用普通文本回答,必须调用本工具。", skillId),
			Parameters: map[string]any{
				"type":       "object",
				"properties": properties,
				"required":   []string{"decision", "reason"},
			},
		},
	}
}

// ================================ parse intention tool arguments==============================

type negotiationStartArgument struct {
	Decision string          `json:"decision"`
	Reason   string          `json:"reason"`
	Missing  []string        `json:"missing"`
	Spec     json.RawMessage `json:"spec"` // 业务参数,保持原样
}

// 对intentionNegotiation 的参数进行解析,形成NegotiationResult,校验在biz层进行
func parseNegotiationStartArg(raw string, spec INegotiationSpec) (NegotiationResult, error) {
	var args negotiationStartArgument
	if err := pCommon.UnmarshalJson(raw, &args); err != nil {
		return NegotiationResult{}, fmt.Errorf("negotiation: invalid argument: %w", err)
	}

	decision := NegotiationDecision(strings.TrimSpace(args.Decision))
	if !decision.Valid() {
		return NegotiationResult{}, fmt.Errorf("negotiation: unknown decision: %s", args.Decision)
	}

	missing := make([]string, 0, len(args.Missing))
	for _, m := range args.Missing {
		if trimmed := strings.TrimSpace(m); trimmed != pCommon.BlankString {
			missing = append(missing, m)
		}
	}

	decoded, err := decodeNegotiationParams(spec, args.Spec)
	if err != nil {
		return NegotiationResult{}, err
	}
	return NegotiationResult{
		Decision: decision,
		Reason:   strings.TrimSpace(args.Reason),
		Missing:  missing,
		Arg:      decoded,
	}, nil
}

// ================================fs_list/fs_stat ====================================
type fsDirEntry struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Type string `json:"type"`
}

type dirListResult struct {
	Path      string         `json:"path"`
	Exists    bool           `json:"exists"`
	IsDir     bool           `json:"isDir"`
	FileCount int            `json:"fileCount"`
	DirCount  int            `json:"dirCount"`
	ExtCount  map[string]int `json:"extCount"`
	Entries   []fsDirEntry   `json:"entries"`
	Truncated bool           `json:"truncated"`
	Hint      string         `json:"hint"`
}

type dirStatResult struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	IsDir  bool   `json:"isDir"`
	Size   int64  `json:"size"`
}

// 对intentionNegotiation的工具进行解析
func executeFsList(argsRaw, workRoot string, rules toolexecutor.ToolConfigRules) (string, error) {
	var tempArg struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}

	if err := pCommon.UnmarshalJson(argsRaw, &tempArg); err != nil {
		return pCommon.BlankString, fmt.Errorf("negotiation: invalid argument while fs list,error: %w", err)
	}

	resolved, err := rules.CheckReadPath(tempArg.Path, workRoot)
	if err != nil {
		return pCommon.BlankString, err
	}

	result := dirListResult{Path: resolved, ExtCount: make(map[string]int, 8), Entries: make([]fsDirEntry, 0, 16)}
	items, readErr := os.ReadDir(resolved)
	if readErr != nil {
		// 目录不存在时不能报错:模型需要知道"路径不存在",再决定追问还是换路径
		result.Hint = readErr.Error()
		return pCommon.MarshalJson(result)
	}
	if info, statErr := os.Stat(resolved); statErr == nil {
		result.Exists, result.IsDir = true, info.IsDir()
	}

	for _, item := range items {
		if item.IsDir() {
			result.DirCount++
			if len(result.Entries) < maxListEntries {
				result.Entries = append(result.Entries, fsDirEntry{Name: item.Name() + "/", Type: "dir"})
			}
			continue
		}

		info, infoErr := item.Info()
		if infoErr != nil {
			continue
		}

		result.FileCount++
		if ext := strings.ToLower(filepath.Ext(item.Name())); ext != pCommon.BlankString {
			result.ExtCount[ext]++
		}
		if len(result.Entries) < maxListEntries {
			result.Entries = append(result.Entries, fsDirEntry{Name: item.Name() + "/", Size: info.Size(), Type: "file"})
		} else {
			result.Truncated = true
		}
	}

	if tempArg.Recursive {
		// 递归探测
		for _, item := range items {
			if !item.IsDir() || len(result.Entries) >= maxListEntries {
				continue
			}

			subEntries, subErr := os.ReadDir(filepath.Join(resolved, item.Name()))
			if subErr != nil {
				continue
			}

			for _, subEntry := range subEntries {
				if len(result.Entries) >= maxListEntries {
					result.Truncated = true
					break
				}

				if ext := strings.ToLower(filepath.Ext(subEntry.Name())); ext != pCommon.BlankString {
					result.ExtCount[ext]++
				}
				result.Entries = append(result.Entries, fsDirEntry{Name: item.Name() + "/" + subEntry.Name(), Type: "file"})
			}
		}
	}

	if len(result.ExtCount) == 0 {
		result.Hint = "目录内没有发现任何带扩展名的文件"
	}

	return pCommon.MarshalJson(result)
}

func executeFsStat(argsRaw, workRoot string, rules toolexecutor.ToolConfigRules) (string, error) {
	var tempArg struct {
		Path string `json:"path"`
	}
	if err := pCommon.UnmarshalJson(argsRaw, &tempArg); err != nil {
		return pCommon.BlankString, fmt.Errorf("negotiation: invalid argument while fs state,error: %w", err)
	}

	resolved, err := rules.CheckReadPath(tempArg.Path, workRoot)
	if err != nil {
		return pCommon.BlankString, err
	}

	result := dirStatResult{
		Path: resolved,
	}
	info, statErr := os.Stat(resolved)
	if statErr == nil {
		result.Exists, result.IsDir, result.Size = true, info.IsDir(), info.Size()
	}

	return pCommon.MarshalJson(result)
}
