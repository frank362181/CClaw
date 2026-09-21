/**
* 工具集合:包含以下两部分：
*	1.从数据库表t_tool中加载
*	2.从 SKILL.md 声明解析可用工具
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

package tool

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/llmproto"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/platformutility/common"
	pCommon "my.favorite/platformutility/common"
)

// SkillSlot 一个已绑定技能的**技能级**元数据(不含“工具→技能”的反向归属)。
//
// 它是两件事的共同输入:
//   - 意图门:Keywords 命中才允许该技能的 force-call 工具下发 tool_choice=required;
//   - SOP 注入:命中后把 File 交给 prompt 层(ctxmgr.BuildSystemPromptExWithActiveSkills),
//     由 prompt repo 取 SKILL.md 正文注入 system prompt —— 正文**不在这里存**,一份数据
//     只缓存一处(prompt repo 已按文件缓存,含 Content)。
type SkillSlot struct {
	SkillId     string   // 技能目录名(如 weather / npa-report)
	File        string   // SKILL.md 绝对路径(与 prompt repo 的 SkillFile 对齐,用于 SOP 取文)
	Name        string   // front matter name
	Description string   // front matter description
	Keywords    []string // front matter trigger-keywords
	ForcedTools []string // 本技能内 force-call 的工具名
	ToolNames   []string // 本技能贡献的工具名(仅观测/日志用)
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

// FunctionBundle 用来构建function-calling计划使用
type FunctionBundle struct {
	Tools       []llmproto.Tool   // 写一层schema,GetLLMTools的产出
	ForcedNames []string          // 声明了force-call的工具名
	Scripts     map[string]string // 工具名 --> 脚本路径
	Slots       []SkillSlot       // 技能级元数据(意图门 + SOP 注入的输入)
}

// Activate 根据本轮用户问题判定“哪些技能相关”(意图门)。
//
// 规则(宽进严出:宁可少强制,不可乱强制):
//   - 声明了 trigger-keywords 的技能:问题(不区分大小写,子串)命中任一关键词 → hit;
//     否则 miss —— miss 时**抑制**它的 force-call(不再把整轮首轮变成必须调工具)。
//   - 未声明的技能:legacy —— 保持旧行为(允许 force),但不注入正文(无法判定相关性)。
func (b FunctionBundle) Activate(question string) Activation {
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
func (b FunctionBundle) VisibleTools(gate Activation) []llmproto.Tool {
	return b.Tools
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

type FunctionRepository struct {
	mu sync.RWMutex

	systemTools []hCommon.SystemicTool //基础工具

	skillTools map[string]hCommon.SkillTool //skill工具,与一个文件skill.md文件挂钩

	// 用来维护skill中的took
	capacity int
	hits     int
	misses   int
}

var functionRepository = common.GetSingleton(func() *FunctionRepository {
	return &FunctionRepository{
		systemTools: []hCommon.SystemicTool{},
		skillTools:  make(map[string]hCommon.SkillTool),

		capacity: 256, //支持256个skill document
	}
}).Instance()

func GetFunctionRepository() *FunctionRepository {
	return functionRepository
}

// InitializeBaseTool 从数据库中加载基础工具，系统启动时加载
func (repo *FunctionRepository) InitializeBaseTool(tools []dataobject.TTool) {
	if len(tools) == 0 {
		//没有基础工具,系统照样运行
		return
	}

	// 将基础工具解析到agent中
}

// GetLLMTools 每轮对话时,根据skill.md文件加载可调用的工具,返回可调用的工具
// 先解析skill.md是为了对skill.md解析两次
func (repo *FunctionRepository) GetLLMTools(skillRoot string, skillFiles []string) []llmproto.Tool {
	basicTools := repo.collectBasicTools(skillRoot, skillFiles)

	// 转换为llm tool
	result := make([]llmproto.Tool, 0, len(basicTools))
	for _, tool := range basicTools {
		llmTool, ok := hCommon.BasicToolToLLMTool(tool)
		if ok {
			result = append(result, llmTool)
		}
	}
	return result
}

// GetFunctionBundle 每轮对话的所有的function-calling的输入
//
// 三份视图(schema / 强制名 / 脚本路径)全部出自同一次收集(collectBasicToolsWithScripts),
// 保证顺序与过滤口径完全一致:否则会出现"某个工具被过滤掉了、却仍在强制名单里"
// (策略层判为"强制工具不在可用集"→ 静默降级)。
func (repo *FunctionRepository) GetFunctionBundle(skillRoot string, skillFiles []string) FunctionBundle {
	basicTools, skills := repo.collectToolsAndSkills(skillRoot, skillFiles)
	bundle := FunctionBundle{
		Tools:       make([]llmproto.Tool, 0, len(basicTools)),
		ForcedNames: make([]string, 0, len(basicTools)),
		Scripts:     make(map[string]string),
		Slots:       skills,
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
		bundle.Scripts[t.Name] = t.Script //此时执行脚本是绝对路径

		//bundle.Slots = append(bundle.Slots, repo.buildSkillSlot(t))
	}

	bundle.ForcedNames = pCommon.NormalizeStrings(bundle.ForcedNames) //规范化名称,去重等
	return bundle
}

// ===================================== private functions & methods ==================================
func (repo *FunctionRepository) collectBasicTools(skillRoot string, skillFiles []string) []hCommon.BasicTool {
	tools, _ := repo.collectToolsAndSkills(skillRoot, skillFiles)
	return tools
}

// collectToolsAndSkills 收集本轮可用的工具声明与**技能级元数据**。
//
// 两份产物出自同一次遍历、同一份缓存(getToolsFromSkill 按 SKILL.md 指纹命中),
// 因此不需要额外 IO;技能元数据(关键词/正文/强制工具)与工具列表也不会错位。
func (repo *FunctionRepository) collectToolsAndSkills(skillRoot string, skillFiles []string) ([]hCommon.BasicTool, []SkillSlot) {
	var disallowedTools []string
	var skillTools []hCommon.BasicTool
	var skills []SkillSlot

	if len(skillFiles) > 0 {
		skillRootCleaned, err := filepath.Abs(skillRoot)
		if err == nil {
			for _, skillFile := range skillFiles {
				if filepath.Base(skillFile) != hCommon.SkillDocFileName {
					continue
				}

				//解析skill.md文件
				repo.mu.Lock()
				skillTool, ok := repo.getToolsFromSkill(skillRootCleaned, skillFile)
				repo.mu.Unlock()

				//将结果添加到数组中
				if ok {
					skillTools = append(skillTools, skillTool.Tools...)
					disallowedTools = append(disallowedTools, skillTool.DisallowedTools...)
					skills = append(skills, repo.buildSkillSlot(skillTool))
				}
			}
		}
	}

	// 此时无skill文件,仅返回基础工具
	repo.mu.Lock()
	baseTools := repo.getToolsFromBaseTool(disallowedTools) //若disallowedTools为空,则表示所有系统工具均可用
	repo.mu.Unlock()

	result := make([]hCommon.BasicTool, 0, len(baseTools)+len(skillTools))
	result = append(result, skillTools...)
	result = append(result, baseTools...)

	return result, skills
}

// buildSkillSlot 把一个已解析的技能转成技能级元数据(只取“技能自己的”属性)。
func (repo *FunctionRepository) buildSkillSlot(skillTool *hCommon.SkillTool) SkillSlot {
	toolNames := make([]string, 0, len(skillTool.Tools))
	for _, t := range skillTool.Tools {
		if name := strings.TrimSpace(t.Name); name != pCommon.BlankString {
			toolNames = append(toolNames, name)
		}
	}

	return SkillSlot{
		SkillId:     filepath.Base(filepath.Dir(skillTool.SkillFileName)), // 技能目录名(如 weather)
		File:        skillTool.SkillFileName,
		Name:        skillTool.Name,
		Description: skillTool.Description,
		Keywords:    skillTool.TriggerKeywords,
		ForcedTools: skillTool.ForcedTools,
		ToolNames:   toolNames,
	}
}

func (repo *FunctionRepository) getToolsFromBaseTool(disallowedList []string) []hCommon.BasicTool {
	var out []hCommon.BasicTool

	//
	for _, call := range repo.systemTools {
		disallowed := false
		for _, ele := range disallowedList {
			if strings.EqualFold(ele, call.Name) {
				disallowed = true
				break
			}
		}

		if !disallowed {
			fp := pCommon.ComputeFileFingerprint(call.GetSchemaFingerprint())
			if strings.EqualFold(call.SchemaFingerprint, fp) {
				out = append(out, call.Tool)
				continue
			}

			//基础工具对应的schema改动过,重新读取

		}
	}

	return out
}

func (repo *FunctionRepository) getToolsFromSkill(skillRoot, skillFile string) (*hCommon.SkillTool, bool) {
	skillFile, err := pCommon.ResolveFile(skillRoot, skillFile)
	if err != nil {
		return nil, false
	}

	fp := pCommon.ComputeFileFingerprint(skillFile)
	skillTool, ok := repo.skillTools[skillFile]
	if ok {
		if strings.EqualFold(skillTool.GetSchemaFingerprint(), fp) {
			repo.hits++

			return &skillTool, true
		}
	}

	//skill.md文件改动过 或不存在系统中,reload again
	repo.misses++
	var st hCommon.SkillTool
	st, ok = hCommon.ParseSkillDocument(skillFile)
	if !ok {
		return nil, false
	}

	//保护skill document的整体容量,满则整体充值
	st.SkillFingerprint = fp
	if len(repo.skillTools) > repo.capacity {
		repo.skillTools = make(map[string]hCommon.SkillTool)
	}
	repo.skillTools[skillFile] = st

	return &st, true
}
