/**
* Agent 目录路径安全的单元测试(IsSafeDir)
*
* 覆盖:合法相对路径、空/点/双点、绝对路径、越界与冗余路径段。
**/

package common

import (
	"testing"

	pCommon "my.favorite/platformutility/common"
)

func TestIsSafeAgentDir(t *testing.T) {
	valid := []string{
		"default",
		"amc_jk",
		"amc_report",
		"sub/dir",
		"a-b_c",
	}
	for _, dir := range valid {
		if !pCommon.IsSafeDir(dir) {
			t.Errorf("expected safe agent dir, got unsafe:%q", dir)
		}
	}

	invalid := []string{
		"",
		".",
		"..",
		"../x",
		"..//x",
		"a/../b", // 归一化后与原文不一致
		"a//b",   // 冗余分隔符
		"a/./b",  // 冗余 . 段
		"/abs/path",
		"default/",  // 尾部斜杠
		"./default", // 前导 ./
	}
	for _, dir := range invalid {
		if pCommon.IsSafeDir(dir) {
			t.Errorf("expected unsafe agent dir, got safe:%q", dir)
		}
	}
}
