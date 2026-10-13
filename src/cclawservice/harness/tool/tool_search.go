/**
* 工具集合:当前只来自 SKILL.md 的工具声明(front matter)。
*
* SKILL.md 的读盘与 YAML 解析**不在这里**:唯一入口是 prompt repo 的 LoadSkills(按文件指纹缓存)。
* 本层只把已解析的文档派生成:LLM 侧工具 schema、脚本绝对路径、技能级元数据(意图门用)。
**/

package tool

import (
	"fmt"
	"path/filepath"
	"strings"

	"my.favorite/cclawservice/harness/api/llmproto"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/ctxmgr"
	"my.favorite/platformutility/common"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"
)

// SkillSlot 一个已绑定技能的**技能级**元数据(不含“工具→技能”的反向归属)。
//
// 它是两件事的共同输入:
//   - 意图门:Keywords 命中才允许该技能的 force-call 工具下发 tool_choice=required;
//   - SOP 注入:命中后把 File 交给 prompt 层(ctxmgr.BuildSystemPromptExWithActiveSkills),
//     由 prompt repo 取 SKILL.md 正文注入 system prompt —— 正文**不在这里存**,一份数据
//     只缓存一处(prompt repo 已按文件缓存,含 Content)。
type SkillSlot struct {
	SkillId         string   // 技能目录名(如 weather / npa-report)
	File            string   // SKILL.md 绝对路径(与 prompt repo 的 SkillFile 对齐,用于 SOP 取文)
	Name            string   // front matter name
	Description     string   // front matter description
	AllowedTools    []string //被allowed的系统工具列表
	DisallowedTools []string // disallowed的系统工具列表
	Keywords        []string // front matter trigger-keywords
	ForcedTools     []string // 本技能内 force-call 的工具名
	ToolNames       []string // 本技能贡献的工具名(仅观测/日志用)
}

// Activation 一轮对话的“技能激活”结果(意图门)。
//   - ActiveSkills:命中的技能(用于 SOP 注入与日志)
//   - ForcedTools:命中技能内声明 force-call 的工具名(仅这些允许强制)
//   - SuppressedForce:因未命中而被抑制的强制工具(观测用:证明门生效了)
//   - Legacy:未声明触发词的技能(保持旧行为:允许 force,但不注入正文)
type Activation struct {
	ActiveSkills    []string
	ActiveFiles     []string
	ForcedTools     []string
	SuppressedForce []string
	Legacy          []string
	HitKeywords     []string
}

// ToolsBundle 用来构建function-calling计划使用
type ToolsBundle struct {
	Tools       []llmproto.Tool   // 下发给 LLM 的工具 schema(由 SKILL.md 文档派生)
	ForcedNames []string          // 声明了force-call的工具名
	Scripts     map[string]string // 工具名 --> 脚本路径(绝对路径)
	Slots       []SkillSlot       // 技能级元数据(意图门 + SOP 注入的输入)
}

// Activate 根据本轮用户问题判定“哪些技能相关”(意图门)
func (b ToolsBundle) Activate(question string) Activation {
	act := Activation{}
	q := strings.ToLower(strings.TrimSpace(question))

	for i := range b.Slots {
		slot := &b.Slots[i]

		// 未声明关键词:保持旧行为(允许强制),不进 SOP
		if len(slot.Keywords) == 0 {
			act.Legacy = append(act.Legacy, slot.SkillId)
			act.ForcedTools = append(act.ForcedTools, slot.ForcedTools...)
			continue
		}

		hit := false
		for _, kw := range slot.Keywords {
			kw = strings.TrimSpace(kw)
			if kw == pCommon.BlankString {
				continue
			}
			if strings.Contains(q, strings.ToLower(kw)) {
				hit = true
				act.HitKeywords = append(act.HitKeywords, slot.SkillId+":"+kw)
			}
		}

		if !hit {
			act.SuppressedForce = append(act.SuppressedForce, slot.ForcedTools...)
			continue
		}

		act.ActiveSkills = append(act.ActiveSkills, slot.SkillId)
		act.ActiveFiles = append(act.ActiveFiles, slot.File)
		act.ForcedTools = append(act.ForcedTools, slot.ForcedTools...)
	}

	act.ActiveSkills = pCommon.NormalizeStrings(act.ActiveSkills)
	act.ActiveFiles = pCommon.NormalizeStrings(act.ActiveFiles)
	act.ForcedTools = pCommon.NormalizeStrings(act.ForcedTools)
	act.SuppressedForce = pCommon.NormalizeStrings(act.SuppressedForce)
	act.Legacy = pCommon.NormalizeStrings(act.Legacy)
	return act
}

// VisibleTools 返回本轮实际下发给LLM的工具列表
func (b ToolsBundle) VisibleTools(gate Activation) []llmproto.Tool {
	return b.Tools
}

// PromptItems 把**本轮实际下发的工具**投影成 system prompt 的清单项(名字 + 一句话用途 + 归属技能)。
// 【唯一事实源】只读 b.Tools(即 request.tools 的来源)与 b.Slots(同一批过滤后的名字):
// 两者在生产端就已对齐(见 collectToolsFromSkill 的注释),所以 prompt 清单不可能与 schema 漂移，且顺序与slots一致
func (b ToolsBundle) PromptItems() []hCommon.ToolPromptItem {
	owner := make(map[string]string, len(b.Tools))
	for i := range b.Slots {
		for _, name := range b.Slots[i].ToolNames {
			if _, dup := owner[name]; !dup {
				owner[name] = b.Slots[i].SkillId
			}
		}
	}

	items := make([]hCommon.ToolPromptItem, 0, len(b.Tools))
	for i := range b.Tools {
		name := strings.TrimSpace(b.Tools[i].Function.Name)
		if name == pCommon.BlankString {
			continue
		}

		item := hCommon.ToolPromptItem{
			Name:        name,
			Description: strings.TrimSpace(b.Tools[i].Function.Description),
			Source:      hCommon.TSourceSystem, // 默认按系统工具,命中技能声明再改写
		}
		if skillId, ok := owner[name]; ok {
			item.Source = hCommon.TSourceSkill
			item.SkillId = skillId
		}

		items = append(items, item)
	}

	return items
}

// String 便于日志一眼核对门的结果。
func (a Activation) String() string {
	desc := fmt.Sprintf("active=%v keyword=%v forced=%v", a.ActiveSkills, a.HitKeywords, a.ForcedTools)
	if len(a.SuppressedForce) > 0 {
		desc += " suppressed_force=" + strings.Join(a.SuppressedForce, ",")
	}
	if len(a.Legacy) > 0 {
		desc += " legacy_no_keywords=" + strings.Join(a.Legacy, ",")
	}
	return desc
}

// ToolsSearch 工具仓库。
// 包含skill tool和system tool
type ToolsSearch struct{}

var toolsSearch = common.GetSingleton(func() *ToolsSearch {
	return &ToolsSearch{}
}).Instance()

func GetToolsSearch() *ToolsSearch {
	return toolsSearch
}

// GetToolsBundle 每轮对话的所有的 function-calling 输入。
// 根据skill.md文件中定义的allowed,disallowed加载系统工具
func (search *ToolsSearch) GetToolsBundle(skillDocs []hCommon.SkillDocument) ToolsBundle {
	basicTools, slots := search.collectToolsFromSkill(skillDocs)
	bundle := ToolsBundle{
		Tools:       make([]llmproto.Tool, 0, len(basicTools)),
		ForcedNames: make([]string, 0, len(basicTools)),
		Scripts:     make(map[string]string),
		Slots:       slots,
	}

	for _, t := range basicTools {
		llmTool, ok := hCommon.BasicToolToLLMTool(t)
		if !ok {
			continue
		}

		bundle.Tools = append(bundle.Tools, llmTool)
		if t.ForceCall {
			// 用转换后的名字(已 TrimSpace),与 schema 名、模型回传名严格同源
			bundle.ForcedNames = append(bundle.ForcedNames, llmTool.Function.Name)
		}
		bundle.Scripts[strings.TrimSpace(t.Name)] = t.Script //此时执行脚本是绝对路径
	}

	bundle.ForcedNames = pCommon.NormalizeStrings(bundle.ForcedNames) //规范化名称,去重等
	return bundle
}

// Destroy 生命周期钩子(ServiceMain.Destroy 会调用):本仓库当前无自有状态,无需释放。
func (search *ToolsSearch) Destroy() {}

// ===================================== private functions & methods ==================================

// collectToolsFromSkill 由已解析的技能文档收集工具声明与**技能级元数据**。
// 技能元数据(关键词/强制工具)与工具列表也不可能错位:
//   - 只有**解析出脚本绝对路径**的工具才进入 tools —— 其余既不下发 schema,也不进
//     forcedTools(否则意图门会强制模型去调一个必然失败的工具);
//   - buildSkillSlot 复用同一份 tools,保证 ToolNames 与实际下发的工具严格同源。
func (search *ToolsSearch) collectToolsFromSkill(skillDocs []hCommon.SkillDocument) ([]hCommon.BasicTool, []SkillSlot) {
	var visibleTools []hCommon.BasicTool
	var slots []SkillSlot

	for i := range skillDocs {
		doc := skillDocs[i]

		// 未落盘的文档没有 SKILL.md 路径:脚本目录无从谈起,整份跳过
		// (否则 filepath.Dir("")="." 会按进程 CWD 去解析)
		if strings.TrimSpace(doc.SkillFile) == pCommon.BlankString {
			logging.LogWarnF("tool repository: skill %s has no SKILL.md path, skip", doc.SkillId)
			continue
		}
		baseDir := filepath.Dir(doc.SkillFile)

		tools := make([]hCommon.BasicTool, 0, len(doc.FrontMatter.Tools))
		var forcedTools []string

		for j := range doc.FrontMatter.Tools {
			t := doc.FrontMatter.Tools[j]

			toolName := strings.TrimSpace(t.Name)
			if toolName == pCommon.BlankString {
				continue // 工具名称为空即放弃
			}

			// 脚本是技能工具的唯一执行体:未声明/文件不存在就不下发。
			// 保留一个空脚本的 schema 只会让模型调用一个必然失败的工具,force-call 更是 100% 失败。
			resolved, err := pCommon.SafeJoinWithRoot(baseDir, t.Script)
			if err != nil {
				logging.LogWarnF("tool repository: skill %s tool %s script %q unavailable, tool dropped: %s",
					doc.SkillId, toolName, t.Script, err.Error())
				continue
			}

			t.Script = resolved // 绝对路径
			t.Source = hCommon.TSourceSkill
			tools = append(tools, t)

			if t.ForceCall {
				forcedTools = append(forcedTools, toolName)
			}
		}

		visibleTools = append(visibleTools, tools...)

		// 将元数据提取出来(与上面下发的 tools 同源)
		slots = append(slots, buildSkillSlot(doc, tools, forcedTools))
	}

	// 已经从skill.md中解析出allowed-tools,disallowed-tools等对系统工具的调用属性
	var allowedTools []string
	var disabledTools []string
	for _, slot := range slots {
		allowedTools = append(allowedTools, slot.AllowedTools...)
		disabledTools = append(disabledTools, slot.DisallowedTools...)
	}

	documents, err := ctxmgr.GetAgentDocCache().GetSystemToolSchemaDocument(allowedTools, disabledTools)
	if err != nil {
		logging.LogWarnF("failed to load system tool for skills: %s", err.Error())
	}

	for _, doc := range documents {
		scriptFile := strings.TrimSpace(doc.Script)
		if scriptFile == pCommon.BlankString {
			logging.LogWarnF("tool repository: system tool %s has no resolved script, skip", doc.ToolName)
			continue
		}

		sysTool := hCommon.BasicTool{
			Source:      hCommon.TSourceSystem,
			Name:        strings.ToLower(strings.TrimSpace(doc.ToolName)),
			Description: doc.SysToolSchemaMeta.Description,
			Script:      scriptFile,
			Parameters:  doc.SysToolSchemaMeta.InputSchema,
		}

		visibleTools = append(visibleTools, sysTool)
	}

	return visibleTools, slots
}

// buildSkillSlot 把一个已解析的技能转成技能级元数据(只取“技能自己的”属性)。
//
// tools 必须是 collectToolsAndSkills 实际下发的派生结果,而不是原始 front matter:
// 空名工具、脚本解析失败的工具都已被过滤,两边口径必须一致。
func buildSkillSlot(doc hCommon.SkillDocument, tools []hCommon.BasicTool, forcedTools []string) SkillSlot {
	toolNames := make([]string, 0, len(tools))
	for _, t := range tools {
		if name := strings.TrimSpace(t.Name); name != pCommon.BlankString {
			toolNames = append(toolNames, name)
		}
	}

	return SkillSlot{
		SkillId:         doc.SkillId,
		File:            doc.SkillFile,
		Name:            strings.TrimSpace(doc.FrontMatter.Name),
		Description:     strings.TrimSpace(doc.FrontMatter.Description),
		AllowedTools:    pCommon.NormalizeStrings(doc.FrontMatter.AllowedTools),
		DisallowedTools: pCommon.NormalizeStrings(doc.FrontMatter.DisallowedTools),
		Keywords:        pCommon.NormalizeStrings(doc.FrontMatter.TriggerKeywords),
		ForcedTools:     pCommon.NormalizeStrings(forcedTools),
		ToolNames:       pCommon.NormalizeStrings(toolNames),
	}
}
