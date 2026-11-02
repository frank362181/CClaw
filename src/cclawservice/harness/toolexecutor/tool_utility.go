package toolexecutor

import (
	"path/filepath"
	"strings"

	pCommon "my.favorite/platformutility/common"
)

// 从已校验的参数对象里取出全部路径字段的原始值
func extractToolPath(spec ToolSpec, input map[string]any) []string {
	if len(spec.PathFields) == 0 || len(input) == 0 {
		return nil
	}

	out := make([]string, 0, len(spec.PathFields))
	for _, field := range spec.PathFields {
		raw, ok := input[field]
		if !ok {
			continue
		}

		s, ok := raw.(string)
		if !ok || strings.TrimSpace(s) == pCommon.BlankString {
			continue
		}

		out = append(out, s)
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

// 判断 realPath是否落在rootDirs中,若命中返回命中的root
func toolWithinRoots(realPath string, rootDirs []string) (bool, string) {
	if strings.TrimSpace(realPath) == pCommon.BlankString {
		return false, pCommon.BlankString
	}

	for _, rootDir := range rootDirs {
		r := strings.TrimSpace(rootDir)
		if r == pCommon.BlankString || !filepath.IsAbs(r) {
			continue
		}

		if _, err := pCommon.SafeJoinWithRoot(r, realPath); nil == err {
			return true, r
		}
	}

	return false, pCommon.BlankString
}

// 所有路径是否都落在 rootDirs中,是则返回true
func allToolWithinRoots(resolved []string, rootDirs []string) bool {
	if len(rootDirs) == 0 {
		return false
	}

	for _, realPath := range resolved {
		if ok, _ := toolWithinRoots(realPath, rootDirs); !ok {
			return false
		}
	}

	return true
}

// 命中规则则返回（true,命中原始规则串)
// 支持两种写法,按 "是否是绝对路径" 自动分类
func matchDenyRoot(resolved []string, denyRoots []string) (bool, string) {
	if len(denyRoots) == 0 {
		return false, pCommon.BlankString
	}

	for _, realPath := range resolved {
		base := strings.ToLower(filepath.Base(realPath))

		for _, raw := range denyRoots {
			d := strings.TrimSpace(raw)
			if d == pCommon.BlankString {
				continue
			}

			d = pCommon.NormalizePath(d, "", true, true, false, true)
			if filepath.IsAbs(d) {
				if ok, _ := toolWithinRoots(realPath, []string{d}); ok {
					return true, raw
				}

				continue
			}

			d = strings.ToLower(d)
			if base == d || strings.HasSuffix(base, d) {
				return true, raw
			}
		}
	}

	return false, pCommon.BlankString
}
