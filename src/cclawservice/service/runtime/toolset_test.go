/**
* ToolSet 单元测试:SKILL.md 解析 + 脚本执行(本地临时目录,无网络依赖)
**/

package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pCommon "my.favorite/platformutility/common"
)

// writeSkillDoc 在临时目录构造一个含 tools 声明的 SKILL.md,返回其绝对路径。
func writeSkillDoc(t *testing.T, extRoot, skillName, toolYAML, scriptRel string, withScript bool) string {
	t.Helper()
	skillDir := filepath.Join(extRoot, skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	doc := "---\nname: " + skillName + "\ndescription: test skill\ntools:\n" + toolYAML + "---\n\n# content\n"
	skillFile := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte(doc), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if withScript && scriptRel != "" {
		scriptAbs := filepath.Join(skillDir, scriptRel)
		if err := os.MkdirAll(filepath.Dir(scriptAbs), 0o755); err != nil {
			t.Fatalf("mkdir script dir: %v", err)
		}
		py := "import sys, json\nargs = json.load(sys.stdin)\nprint(json.dumps({\"echo\": args.get(\"city\", \"\")}, ensure_ascii=False))\n"
		if err := os.WriteFile(scriptAbs, []byte(py), 0o755); err != nil {
			t.Fatalf("write script: %v", err)
		}
	}
	return skillFile
}

// TestLoadToolSet_ResolveTool 验证从 SKILL.md 解析出工具声明。
func TestLoadToolSet_ResolveTool(t *testing.T) {
	extRoot := t.TempDir()
	toolYAML := "  - name: get_weather\n    description: 查询天气\n    script: scripts/weather.py\n    parameters:\n      type: object\n      properties:\n        city:\n          type: string\n      required:\n        - city\n"
	skillFile := writeSkillDoc(t, extRoot, "weather", toolYAML, "scripts/weather.py", true)

	ts := LoadToolSet(extRoot, []string{skillFile})
	if len(ts.LLMTools()) != 1 {
		t.Fatalf("LLMTools len = %d, want 1", len(ts.LLMTools()))
	}
	fn := ts.LLMTools()[0].Function
	if fn.Name != "get_weather" || fn.Description != "查询天气" {
		t.Errorf("unexpected tool fn: %+v", fn)
	}
	// Parameters 应可 JSON 化(嵌套 map 已转 string key)
	paramsJSON, err := jsonMarshal(fn.Parameters)
	if err != nil {
		t.Fatalf("parameters not JSON-marshalable: %v", err)
	}
	if !strings.Contains(paramsJSON, `"city"`) || !strings.Contains(paramsJSON, `"required"`) {
		t.Errorf("parameters missing schema: %s", paramsJSON)
	}
}

// TestToolSet_ExecScript 验证执行脚本并取回结果。
func TestToolSet_ExecScript(t *testing.T) {
	extRoot := t.TempDir()
	toolYAML := "  - name: get_weather\n    description: 查询天气\n    script: scripts/weather.py\n"
	skillFile := writeSkillDoc(t, extRoot, "weather", toolYAML, "scripts/weather.py", true)

	ts := LoadToolSet(extRoot, []string{skillFile})
	if len(ts.LLMTools()) != 1 {
		t.Fatalf("LLMTools len = %d, want 1", len(ts.LLMTools()))
	}
	out, err := ts.Exec(context.Background(), "get_weather", `{"city":"杭州"}`)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if !strings.Contains(out, `"杭州"`) {
		t.Errorf("exec output = %q, want echo 杭州", out)
	}
}

// TestLoadToolSet_MissingScript 验证脚本缺失时该工具被跳过(不中断其他工具)。
func TestLoadToolSet_MissingScript(t *testing.T) {
	extRoot := t.TempDir()
	// 工具A脚本存在,工具B脚本缺失
	toolYAML := "  - name: tool_a\n    description: a\n    script: scripts/a.py\n  - name: tool_b\n    description: b\n    script: scripts/b.py\n"
	skillFile := writeSkillDoc(t, extRoot, "multi", toolYAML, "scripts/a.py", true)

	ts := LoadToolSet(extRoot, []string{skillFile})
	if len(ts.LLMTools()) != 1 || ts.LLMTools()[0].Function.Name != "tool_a" {
		t.Fatalf("want only tool_a registered, got %+v", ts.LLMTools())
	}
	if _, err := ts.Exec(context.Background(), "tool_b", `{}`); err == nil {
		t.Error("exec of unregistered tool_b should fail")
	}
}

// TestLoadToolSet_RejectTraversal 验证脚本越界(../ 逃逸 extensionRoot)被拒绝。
func TestLoadToolSet_RejectTraversal(t *testing.T) {
	extRoot := t.TempDir()
	toolYAML := "  - name: evil\n    description: x\n    script: ../../evil.py\n"
	skillFile := writeSkillDoc(t, extRoot, "evil", toolYAML, "", false)
	// 在 extensionRoot 之外放一个真实存在的脚本
	outside := filepath.Join(filepath.Dir(extRoot), "evil.py")
	if err := os.WriteFile(outside, []byte("print('hi')\n"), 0o755); err != nil {
		t.Fatalf("write outside script: %v", err)
	}

	ts := LoadToolSet(extRoot, []string{skillFile})
	if len(ts.LLMTools()) != 0 {
		t.Fatalf("traversal script should be rejected, got %+v", ts.LLMTools())
	}
}

// TestToJSONMap 验证 yaml 嵌套 map 转 JSON 可序列化结构(转换已收敛到 pCommon.YamlToJsonMap)。
func TestToJSONMap(t *testing.T) {
	in := map[string]any{
		"type": "object",
		"nested": map[interface{}]interface{}{
			"a": []interface{}{1, 2},
		},
	}
	out := pCommon.YamlToJsonMap(in)
	nested, ok := out["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested not map[string]any: %T", out["nested"])
	}
	_ = nested
	if _, err := jsonMarshal(out); err != nil {
		t.Fatalf("json marshal failed: %v", err)
	}
}

// jsonMarshal 使用 encoding/json 序列化(工具集内没有公开该函数,直接引用标准库)。
func jsonMarshal(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

// TestToolSetCache_ReuseOnUnchanged 验证 SKILL.md 未变化时命中指纹缓存(不重复解析)。
func TestToolSetCache_ReuseOnUnchanged(t *testing.T) {
	extRoot := t.TempDir()
	toolYAML := "  - name: get_weather\n    description: 查询天气\n    script: scripts/weather.py\n"
	skillFile := writeSkillDoc(t, extRoot, "weather", toolYAML, "scripts/weather.py", true)

	ts1 := LoadToolSet(extRoot, []string{skillFile})
	if len(ts1.LLMTools()) != 1 {
		t.Fatalf("first load tools len = %d, want 1", len(ts1.LLMTools()))
	}
	hitsAfterFirst := skillToolsCache.hits
	missesAfterFirst := skillToolsCache.misses
	if missesAfterFirst < 1 {
		t.Errorf("expected >=1 cache miss on first load, got %d", missesAfterFirst)
	}

	// 文件未变化 → 应命中缓存且结果一致
	ts2 := LoadToolSet(extRoot, []string{skillFile})
	if len(ts2.LLMTools()) != 1 || ts2.LLMTools()[0].Function.Name != "get_weather" {
		t.Fatalf("second load unexpected: %+v", ts2.LLMTools())
	}
	if skillToolsCache.hits <= hitsAfterFirst {
		t.Errorf("expected a cache hit on unchanged file, hits %d -> %d", hitsAfterFirst, skillToolsCache.hits)
	}
}

// TestToolSetCache_InvalidateOnChange 验证 SKILL.md 变更(mtime 变化)后重新解析。
func TestToolSetCache_InvalidateOnChange(t *testing.T) {
	extRoot := t.TempDir()
	toolYAML := "  - name: get_weather\n    description: 查询天气\n    script: scripts/weather.py\n"
	skillFile := writeSkillDoc(t, extRoot, "weather", toolYAML, "scripts/weather.py", true)

	if ts := LoadToolSet(extRoot, []string{skillFile}); len(ts.LLMTools()) != 1 {
		t.Fatalf("initial load tools len = %d, want 1", len(ts.LLMTools()))
	}

	// 修改声明为另一个工具(写入后强制推进 mtime,确保指纹变化)
	newDoc := "---\nname: weather\ndescription: x\ntools:\n  - name: another_tool\n    description: x\n    script: scripts/weather.py\n---\n# changed\n"
	if err := os.WriteFile(skillFile, []byte(newDoc), 0o644); err != nil {
		t.Fatalf("rewrite SKILL.md: %v", err)
	}
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(skillFile, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	ts2 := LoadToolSet(extRoot, []string{skillFile})
	if len(ts2.LLMTools()) != 1 || ts2.LLMTools()[0].Function.Name != "another_tool" {
		t.Fatalf("expected re-parsed tool another_tool, got %+v", ts2.LLMTools())
	}
}

// TestScriptPolicy_Defaults 验证默认护栏策略非零且与安全常量一致。
// (ScriptPolicy 零值字段不回填——回退发生在 runScriptWithPolicy 内部,由 TestRunScriptWithPolicy_CustomTimeout 覆盖。)
func TestScriptPolicy_Defaults(t *testing.T) {
	p := DefaultScriptPolicy
	if p.Bin == "" || p.Timeout <= 0 || p.MaxOutput <= 0 || p.MaxInputJSON <= 0 {
		t.Fatalf("default policy must be non-zero, got %+v", p)
	}
	if p.Bin != scriptBin || p.Timeout != scriptRunTimeout || p.MaxOutput != scriptMaxOutput || p.MaxInputJSON != scriptMaxInputJSON {
		t.Errorf("default policy mismatch: %+v", p)
	}
}

// TestRunScriptWithPolicy_CustomTimeout 验证自定义策略超时生效。
func TestRunScriptWithPolicy_CustomTimeout(t *testing.T) {
	extRoot := t.TempDir()
	// 一个挂起(睡眠 5s)的脚本
	scriptPath := filepath.Join(extRoot, "hang.py")
	if err := os.WriteFile(scriptPath, []byte("import time\ntime.sleep(5)\nprint('done')\n"), 0o755); err != nil {
		t.Fatalf("write hang script: %v", err)
	}

	policy := DefaultScriptPolicy
	policy.Timeout = 100 * time.Millisecond
	start := time.Now()
	_, err := runScriptWithPolicy(context.Background(), policy, scriptPath, "{}")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected timeout error message, got: %v", err)
	}
	if elapsed > 3*time.Second {
		t.Errorf("timeout took too long: %v", elapsed)
	}
}
