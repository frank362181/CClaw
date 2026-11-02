/**
* 系统工具的执行期的固定画像**
* [覆盖范围]:仅限于系统工具
**/

package toolexecutor

import "strings"

type SystemToolProfile struct {
	ReadOnly   bool
	Sandboxed  bool
	PathFields []string
	Shelled    bool
}

var systemToolProfiles = map[string]SystemToolProfile{
	SystemToolRead.String():  {ReadOnly: true, PathFields: []string{"file_path"}},
	SystemToolGlob.String():  {ReadOnly: true, PathFields: []string{"path"}},
	SystemToolGrep.String():  {ReadOnly: true, PathFields: []string{"path"}},
	SystemToolWrite.String(): {Sandboxed: true, PathFields: []string{"file_path"}},
	SystemToolEdit.String():  {Sandboxed: true, PathFields: []string{"file_path"}},
	SystemToolBash.String():  {Sandboxed: true, Shelled: true},
}

// 未知系统工具的固定配置,兜底
// fail-safe:宁可多一层沙箱,不可漏放一个可能写磁盘的命令
var fallbackSystemToolProfile = SystemToolProfile{Sandboxed: true, Shelled: true}

func LookupSystemToolProfile(toolName string) SystemToolProfile {
	profile, ok := systemToolProfiles[strings.ToLower(strings.TrimSpace(toolName))]
	if !ok {
		return fallbackSystemToolProfile // 未知的系统工具:可写 + 必须沙箱
	}
	return profile
}
