/**
* 启动文档路径解析的单元测试。
*
* 技能文档路径解析(ResolveSkillDocPath/BuildSkillRoot/NormalizeSkillDir)已随
* "仓库/缓存分离"迁至 harness/ctxmgr,其测试见 harness/ctxmgr/ctxmgr_utility_test.go
* (harness/common 不能反向 import ctxmgr,会成环)。
**/

package common

import (
	"path/filepath"
	"testing"
)

// TestResolveStartupPath 启动文档只允许纯文件名,越界/带路径分隔符必须被拒(SafeJoin 收敛后的回归)。
func TestResolveStartupPath(t *testing.T) {
	root := t.TempDir()

	got, err := ResolveStartupPath(root, AgentDocFileName)
	if err != nil {
		t.Fatalf("legal startup file name should resolve: %v", err)
	}
	if got != filepath.Join(root, AgentDocFileName) {
		t.Errorf("resolved path mismatch: got=%q", got)
	}

	for _, name := range []string{"../" + AgentDocFileName, "sub/" + AgentDocFileName, ".", "..", ""} {
		if _, err := ResolveStartupPath(root, name); err == nil {
			t.Errorf("illegal startup file name %q should be rejected", name)
		}
	}
}
