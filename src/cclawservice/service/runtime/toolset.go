/**
* 工具集合(ToolSet):从 SKILL.md 声明解析可用工具
*
* 目的:"加一个 SKILL.md + scripts/xx.py = 一个新工具,免重编译"。
*   每个 SKILL.md 通过 front matter 的 tools 声明 0..N 个工具;
*   每个工具声明 name/description/parameters(生成 LLM function schema)与 script(相对 SKILL.md 的脚本路径)。
*
* 职责:
*   - LoadToolSet:扫描某 Agent 启用的 skill 文件,解析 tools 声明,建立 工具名 → 脚本绝对路径 映射;
*   - LLMTools:返回全部工具声明(供 function-calling schema);
*   - Exec:按工具名执行对应脚本(stdin 传参数 JSON,stdout 取结果)。
*
* 安全:脚本路径必须解析在 agent extension 根内(防目录穿越/符号链接逃逸);
* 单个 SKILL.md 解析失败或脚本缺失时跳过该工具,不影响其余工具(与"缺失技能不影响主流程"语义一致)。
**/

package runtime

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v2"
	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/model/dataobject"

	myUtility "my.favorite/cclawservice/utility"
	pCommon "my.favorite/platformutility/common"
)

// ToolSet 是某 Agent 本次会话可用的工具集合。
type ToolSet struct {
	tools []Tool            // 提供给 LLM 的函数声明
	exec  map[string]string // 工具名 → 脚本绝对路径
}

// LoadToolSet 扫描 skillFiles(每个都是 SKILL.md 绝对路径),解析其中声明的工具。
// extensionRoot 用于安全校验:脚本必须解析在该目录之内。
// 解析结果按 (size,mtime) 指纹缓存:SKILL.md 未变化时复用,避免每次聊天重读文件+YAML。
func LoadToolSet(extensionRoot string, skillFiles []string) *ToolSet {
	ts := &ToolSet{exec: make(map[string]string)}
	if extensionRoot == "" {
		return ts
	}
	rootClean, err := filepath.Abs(extensionRoot)
	if err != nil {
		return ts
	}

	for _, skillFile := range skillFiles {
		if filepath.Base(skillFile) != cCommon.SkillDocFileName {
			continue
		}
		for _, pt := range getSkillToolsCached(rootClean, skillFile) {
			ts.tools = append(ts.tools, pt.tool)
			ts.exec[pt.name] = pt.script
		}
	}
	return ts
}

// LLMTools 返回全部工具声明(供 LLM function-calling schema)。
func (ts *ToolSet) LLMTools() []Tool { return ts.tools }

// Exec 执行指定工具:校验注册 → 运行脚本 → 返回结果文本。
func (ts *ToolSet) Exec(ctx context.Context, name, argsJSON string) (string, error) {
	scriptPath, ok := ts.exec[name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	return RunScript(ctx, scriptPath, argsJSON)
}

// ExecStream 流式执行指定工具:脚本 stdout 逐行回调 onLine(供全流式工具进度),
// 返回完整输出与错误。onLine 为空时退化为普通 Exec(一次性取结果)。
func (ts *ToolSet) ExecStream(ctx context.Context, name, argsJSON string, onLine func(string)) (string, error) {
	scriptPath, ok := ts.exec[name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	if onLine == nil {
		return RunScript(ctx, scriptPath, argsJSON)
	}
	return RunScriptStream(ctx, scriptPath, argsJSON, onLine)
}

// parsedTool 是单个 SKILL.md 解析出的一个工具(内部缓存单元)。
type parsedTool struct {
	name   string
	script string
	tool   Tool
}

// skillToolsCacheEntry 缓存条目:SKILL.md 的 (size,mtime) 指纹 + 解析出的工具。
type skillToolsCacheEntry struct {
	fingerprint string
	tools       []parsedTool
}

// skillToolsCache 工具解析缓存:(size,mtime) 指纹命中时跳过读文件 + YAML 解析。
// hits/misses 仅用于测试与观测;容量满时整体重置(简单可靠)。
var skillToolsCache = struct {
	mu       sync.Mutex
	entries  map[string]skillToolsCacheEntry
	capacity int
	hits     int
	misses   int
}{entries: make(map[string]skillToolsCacheEntry), capacity: 256}

// getSkillToolsCached 读取某 SKILL.md 的工具列表,优先走指纹缓存。
// 缓存未命中时在锁外解析(避免持锁做磁盘 IO),再回填缓存。
func getSkillToolsCached(rootClean, skillFile string) []parsedTool {
	fp := pCommon.ComputeFileFingerprint(skillFile)

	skillToolsCache.mu.Lock()
	if e, ok := skillToolsCache.entries[skillFile]; ok && e.fingerprint == fp {
		skillToolsCache.hits++
		skillToolsCache.mu.Unlock()
		return e.tools
	}
	skillToolsCache.mu.Unlock()

	tools := parseSkillTools(rootClean, skillFile)

	skillToolsCache.mu.Lock()
	skillToolsCache.misses++
	if len(skillToolsCache.entries) >= skillToolsCache.capacity {
		skillToolsCache.entries = make(map[string]skillToolsCacheEntry) // 容量保护:满则整体重置
	}
	skillToolsCache.entries[skillFile] = skillToolsCacheEntry{fingerprint: fp, tools: tools}
	skillToolsCache.mu.Unlock()
	return tools
}

// skillToolToLLMTool 把 SKILL.md 声明的 SkillTool 转换为 LLM 协议层的 Tool。
// 说明:dataobject.SkillTool 是"声明层"(含执行细节 Script,来自 YAML front matter);
// runtime.Tool 是"协议层"(OpenAI tools 项,不含执行细节)。
// 依赖方向为 runtime→dataobject,故转换器放在消费方(runtime)而非 model 层。
// 空工具名返回 ok=false(调用方跳过);脚本路径由调用方另行解析(resolveScriptPath)。
func skillToolToLLMTool(t dataobject.SkillTool) (Tool, bool) {
	name := strings.TrimSpace(t.Name)
	if name == "" {
		return Tool{}, false
	}
	return Tool{
		Type: "function",
		Function: ToolFunction{
			Name:        name,
			Description: strings.TrimSpace(t.Description),
			Parameters:  pCommon.YamlToJsonMap(t.Parameters), // yaml.v2 嵌套 map → 可 JSON 化的 map[string]any
		},
	}, true
}

// parseSkillTools 读取并解析单个 SKILL.md,返回其声明的(脚本可用)工具列表。
func parseSkillTools(rootClean, skillFile string) []parsedTool {
	doc, ok := loadSkillDoc(skillFile)
	if !ok {
		return nil
	}
	skillBaseDir := filepath.Dir(skillFile)
	out := make([]parsedTool, 0, len(doc.FrontMatter.Tools))
	for _, t := range doc.FrontMatter.Tools {
		tool, valid := skillToolToLLMTool(t) // 声明层 → 协议层(单点收敛)
		if !valid {
			continue // 空工具名 → 跳过
		}
		scriptPath, ok := resolveScriptPath(rootClean, skillBaseDir, t.Script)
		if !ok {
			continue // 脚本缺失/越界 → 跳过该工具
		}
		out = append(out, parsedTool{name: tool.Function.Name, script: scriptPath, tool: tool})
	}
	return out
}

// loadSkillDoc 读取并解析单个 SKILL.md(front matter 强类型,复用 dataobject.SkillFrontMatter)。
func loadSkillDoc(skillFile string) (*dataobject.SkillDocument, bool) {
	data, _, err := pCommon.ReadFile(skillFile, cCommon.MaxSkillDocSize)
	if err != nil {
		return nil, false
	}
	frontMatter, _ := myUtility.SplitFrontMatter(data)
	doc := &dataobject.SkillDocument{SkillFile: skillFile}
	if len(frontMatter) > 0 {
		if err := yaml.Unmarshal([]byte(frontMatter), &doc.FrontMatter); err != nil {
			return nil, false
		}
	}
	return doc, true
}

// resolveScriptPath 把 SKILL.md 声明的脚本相对路径解析为绝对路径,确保一定在extension目录内
func resolveScriptPath(rootClean, skillBaseDir, script string) (string, bool) {
	script = strings.TrimSpace(script)
	if script == "" || filepath.IsAbs(script) {
		return "", false
	}
	return pCommon.SafeJoinExisting(rootClean, filepath.Join(skillBaseDir, script))
}
