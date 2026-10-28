/**
* 系统工具 → ToolsBundle 的端到端测试。
*
* 关注点:系统工具的 schema(描述/参数)必须**完整**流到最终下发给模型的 tool schema。
* 这条链路跨了三个模块(缓存解析 → GetSystemToolSchemaDocument → collectToolsFromSkill),
* 中间任何一环丢字段,模型拿到的都是"无描述、无参数"的残缺工具。
**/

package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/ctxmgr"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
)

// tbWrite 写测试夹具文件
func tbWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// tbFixture 建一套"一个技能 + 一个系统工具"的完整夹具,返回技能文档。
func tbFixture(t *testing.T, toolName, skillToolsYAML string) []hCommon.SkillDocument {
	t.Helper()

	// 系统工具:schema + 脚本
	toolRoot := filepath.Join(pCommon.GetExecutableDir(), hCommon.AgentBootstrapDir, hCommon.SystemToolDirRoot)
	toolDir := filepath.Join(toolRoot, toolName)
	schema := `{"name": "` + toolName + `",
  "description": "读取本地文件内容(系统工具)",
  "input_schema": {"type": "object", "properties": {"file_path": {"type": "string"}}, "required": ["file_path"]}}`
	tbWrite(t, filepath.Join(toolDir, "schema.json"), schema)
	tbWrite(t, filepath.Join(toolDir, "scripts", "run.py"), "# stub\n")
	t.Cleanup(func() { _ = os.RemoveAll(toolDir) })

	// 技能:SKILL.md + 自己的脚本
	skillRoot, _ := ctxmgr.BuildSkillRoot()
	skillDir := filepath.Join(skillRoot, "utbundle-skill")
	tbWrite(t, filepath.Join(skillDir, "scripts", "demo.py"), "# stub\n")
	skillDoc := "---\nname: utbundle-skill\ndescription: 夹具技能\n" + skillToolsYAML + "---\n## SOP\n1. 步骤\n"
	skillPath := filepath.Join(skillDir, ctxmgr.SkillDocFileName)
	tbWrite(t, skillPath, skillDoc)
	t.Cleanup(func() { _ = os.RemoveAll(skillDir) })

	// 经统一缓存加载(与生产同一条路径)
	dc := ctxmgr.GetAgentDocCache()
	dc.LoadSystemTools([]dataobject.TTool{{
		ToolName: toolName, SchemaFile: "schema.json", Script: "scripts/run.py", Visible: true,
	}})

	docs, _ := dc.LoadSkills([]string{"utbundle-skill"})
	if len(docs) != 1 {
		t.Fatalf("技能夹具加载失败: %+v", docs)
	}
	return docs
}

// TestSystemToolSchemaFlowsIntoBundle 端到端回归:
// 系统工具必须以下发形态携带 description 与 parameters。
// 若缓存解析环节丢掉 schema 元数据,这里会拿到"空描述 + 空参数"的残缺工具。
func TestSystemToolSchemaFlowsIntoBundle(t *testing.T) {
	toolName := "utbundle-tool"
	docs := tbFixture(t, toolName, "tools:\n  - name: demo_tool\n    script: scripts/demo.py\n    description: 演示技能工具\n")

	bundle := GetToolsSearch().GetToolsBundle(docs)

	// 技能工具应在
	var skillTool *hCommon.BasicTool
	var sysTool *hCommon.BasicTool
	for _, b := range []string{"demo_tool", toolName} {
		if _, ok := bundle.Scripts[b]; !ok {
			t.Errorf("Scripts 缺少工具 %s: %+v", b, bundle.Scripts)
		}
	}

	// 从下发列表里找出两者,核对 schema 完整性
	for i := range bundle.Tools {
		switch bundle.Tools[i].Function.Name {
		case "demo_tool":
			skillTool = toolFromLLM(bundle.Tools[i].Function.Name, bundle.Tools[i].Function.Description, bundle.Tools[i].Function.Parameters)
		case toolName:
			sysTool = toolFromLLM(bundle.Tools[i].Function.Name, bundle.Tools[i].Function.Description, bundle.Tools[i].Function.Parameters)
		}
	}

	if sysTool == nil {
		t.Fatalf("系统工具未下发给模型: %+v", toolNames(bundle))
	}
	if strings.TrimSpace(sysTool.Description) == "" {
		t.Error("系统工具描述丢失 -> 模型无法判断何时调用")
	}
	if len(sysTool.Parameters) == 0 {
		t.Error("系统工具参数丢失 -> 模型无法正确构造入参")
	}
	if _, ok := sysTool.Parameters["properties"]; !ok {
		t.Errorf("参数应保留 input_schema 结构: %+v", sysTool.Parameters)
	}

	if skillTool == nil {
		t.Fatalf("技能工具未下发给模型: %+v", toolNames(bundle))
	}
	if strings.TrimSpace(skillTool.Description) == "" {
		t.Error("技能工具描述丢失")
	}

	// PromptItems 的归属标注:系统工具/技能工具必须区分
	owner := map[string]hCommon.TSource{}
	for _, item := range bundle.PromptItems() {
		owner[item.Name] = item.Source
	}
	if owner[toolName] != hCommon.TSourceSystem {
		t.Errorf("系统工具归属应为 TSourceSystem: %v", owner[toolName])
	}
	if owner["demo_tool"] != hCommon.TSourceSkill {
		t.Errorf("技能工具归属应为 TSourceSkill: %v", owner["demo_tool"])
	}
}

// TestBundleExcludesInvisibleSystemTool 锁定:DB 标记不可见的系统工具不得下发。
func TestBundleExcludesInvisibleSystemTool(t *testing.T) {
	toolName := "utbundle-hidden"
	tbFixture(t, toolName, "tools:\n  - name: demo_tool\n    script: scripts/demo.py\n    description: 演示\n")

	// 覆盖为不可见
	ctxmgr.GetAgentDocCache().LoadSystemTools([]dataobject.TTool{{
		ToolName: toolName, SchemaFile: "schema.json", Script: "scripts/run.py", Visible: false,
	}})

	docs, _ := ctxmgr.GetAgentDocCache().LoadSkills([]string{"utbundle-skill"})
	bundle := GetToolsSearch().GetToolsBundle(docs)

	for _, tool := range bundle.Tools {
		if tool.Function.Name == toolName {
			t.Fatalf("不可见的系统工具不得下发: %+v", toolNames(bundle))
		}
	}
}

// TestBundleRespectsDisallowedTools 锁定:SKILL.md 的 disallowed-tools 必须真的拦住系统工具。
func TestBundleRespectsDisallowedTools(t *testing.T) {
	toolName := "utbundle-disallowed"
	docs := tbFixture(t, toolName,
		"disallowed-tools:\n  - "+toolName+"\ntools:\n  - name: demo_tool\n    script: scripts/demo.py\n    description: 演示\n")

	bundle := GetToolsSearch().GetToolsBundle(docs)
	for _, tool := range bundle.Tools {
		if tool.Function.Name == toolName {
			t.Fatalf("disallowed-tools 声明的系统工具必须被剔除: %+v", toolNames(bundle))
		}
	}
	// 技能自己的工具不受影响
	var hasDemo bool
	for _, tool := range bundle.Tools {
		if tool.Function.Name == "demo_tool" {
			hasDemo = true
		}
	}
	if !hasDemo {
		t.Errorf("技能自身工具不应被 disallowed-tools 影响: %+v", toolNames(bundle))
	}
}

// ============================ 测试辅助 ============================

func toolFromLLM(name, desc string, params map[string]any) *hCommon.BasicTool {
	return &hCommon.BasicTool{Name: name, Description: desc, Parameters: params}
}

func toolNames(bundle ToolsBundle) []string {
	out := make([]string, 0, len(bundle.Tools))
	for i := range bundle.Tools {
		out = append(out, bundle.Tools[i].Function.Name)
	}
	return out
}
