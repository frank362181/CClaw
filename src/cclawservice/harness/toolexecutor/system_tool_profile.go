/**
* 系统工具的执行期的固定画像**
* [覆盖范围]:仅限于系统工具
**/

package toolexecutor

import (
	"strings"

	"my.favorite/cclawservice/common/llmproto"
	"my.favorite/cclawservice/harness/toolcontract"
	pCommon "my.favorite/platformutility/common"
)

var systemToolProfiles = map[string]toolcontract.SystemToolProfile{
	SystemToolRead.String():  {ReadOnly: true, PathFields: []string{"file_path"}},
	SystemToolGlob.String():  {ReadOnly: true, PathFields: []string{"path"}},
	SystemToolGrep.String():  {ReadOnly: true, PathFields: []string{"path"}},
	SystemToolWrite.String(): {Sandboxed: true, PathFields: []string{"file_path"}},
	SystemToolEdit.String():  {Sandboxed: true, PathFields: []string{"file_path"}},
	SystemToolBash.String():  {Sandboxed: true, Shelled: true},
}

// 未知系统工具的固定配置,兜底
// fail-safe:宁可多一层沙箱,不可漏放一个可能写磁盘的命令
var fallbackSystemToolProfile = toolcontract.SystemToolProfile{Sandboxed: true, Shelled: true}

func LookupSystemToolProfile(toolName string) toolcontract.SystemToolProfile {
	profile, ok := systemToolProfiles[strings.ToLower(strings.TrimSpace(toolName))]
	if !ok {
		return fallbackSystemToolProfile // 未知的系统工具:可写 + 必须沙箱
	}
	return profile
}

// GetReadOnlyTools 从工具列表中返回只读的系统工具列表
func GetReadOnlyTools(tools []llmproto.Tool) []llmproto.Tool {
	out := make([]llmproto.Tool, 0, len(tools))
	for _, tool := range tools {
		name := strings.ToLower(strings.TrimSpace(tool.Function.Name))
		if name == pCommon.BlankString {
			continue
		}

		spec := LookupSystemToolProfile(name)
		if !spec.ReadOnly {
			continue // 协商阶段可写的函数不应该出现
		}

		tool.Function.Name = name
		out = append(out, tool)
	}

	return out
}
