/**
* MEMORY.md 人工区/自动区切分的测试。
*
* 切分是"自动区后置注入"的前提:切错了会把蒸馏产物当人工区留在稳定前缀里,
* 于是每次蒸馏都让整段历史缓存失效(这正是本系列改动要消除的问题)。
**/

package common

import (
	"strings"
	"testing"
)

// TestSplitMemoryDoc 人工区与自动区必须严格按定界注释切开。
func TestSplitMemoryDoc(t *testing.T) {
	content := "# MEMORY.md\n\n## 行业规范\n\n- 五级分类:正常/关注/次级/可疑/损失\n\n" +
		"<!-- auto-memory:updated=2026-10-07 10:00:00;watermarks=s1:10 -->\n" +
		"- 单位编码固定为 100001\n" +
		"<!-- /auto-memory -->\n"

	human, auto := SplitMemoryDoc(content)

	if !strings.Contains(human, "五级分类") {
		t.Fatalf("人工区应保留手写内容: %q", human)
	}
	if strings.Contains(human, "100001") || strings.Contains(human, MemoryAutoBeginPrefix) {
		t.Fatalf("人工区不应含自动区内容: %q", human)
	}
	if !strings.Contains(auto, "100001") ||
		!strings.Contains(auto, MemoryAutoBeginPrefix) ||
		!strings.Contains(auto, MemoryAutoEndMarker) {
		t.Fatalf("自动区内容不正确: %q", auto)
	}
}

// TestSplitMemoryDocWithoutAutoRegion 全手写文件:原样作为人工区,自动区为空。
func TestSplitMemoryDocWithoutAutoRegion(t *testing.T) {
	content := "# MEMORY.md\n\n- 全是人工维护的内容\n"
	human, auto := SplitMemoryDoc(content)
	if human != content || auto != "" {
		t.Fatalf("无自动区时应原样返回: human=%q auto=%q", human, auto)
	}
}

// TestAutoRegionBoundsLegacySection 迁移期文件(没有定界注释,只有时间戳小节)
// 也必须被识别为自动区:否则它会被当成人工区进稳定前缀,蒸馏后每轮让缓存失效。
func TestAutoRegionBoundsLegacySection(t *testing.T) {
	content := "# MEMORY.md\n\n## 人工小节\n\n- 手写内容\n\n" +
		"## 2026-09-13 01:02:03 | session s1 | records 1-9\n- 旧格式事实\n"

	lines := strings.Split(content, "\n")
	start, end, ok := AutoRegionBounds(lines)
	if !ok {
		t.Fatalf("旧格式小节应被识别为自动区")
	}
	region := strings.Join(lines[start:end], "\n")
	if !strings.Contains(region, "旧格式事实") || strings.Contains(region, "手写内容") {
		t.Fatalf("自动区范围不正确: %q", region)
	}

	human, auto := SplitMemoryDoc(content)
	if !strings.Contains(human, "手写内容") || strings.Contains(human, "旧格式事实") {
		t.Fatalf("人工区应保留手写内容且不含旧格式事实: %q", human)
	}
	if !strings.Contains(auto, "旧格式事实") {
		t.Fatalf("自动区应含旧格式事实: %q", auto)
	}
}
