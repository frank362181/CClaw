/**
* 工具集合:包含以下两部分：
*	1.从数据库表t_tool中加载
*	2.从 SKILL.md 声明解析可用工具
*
* SKILL.md 的读盘与 YAML 解析**不在这里**:唯一入口是 prompt repo 的 LoadSkills(按文件指纹缓存)。
* 本层只把已解析的文档派生成:LLM 侧工具 schema、脚本绝对路径、技能级元数据(意图门用)。
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
	Tools       []llmproto.Tool   // 下发给 LLM 的工具 schema(由 SKILL.md 文档派生)
	ForcedNames []string          // 声明了force-call的工具名
	Scripts     map[string]string // 工具名 --> 脚本路径(绝对路径)
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

	systemTools map[string]hCommon.SystemicTool //基础工具
}

var functionRepository = common.GetSingleton(func() *FunctionRepository {
	return &FunctionRepository{
		systemTools: make(map[string]hCommon.SystemicTool),
	}
}).Instance()

func GetFunctionRepository() *FunctionRepository {
	return functionRepository
}

// InitializeSystemTool 从数据库中加载基础工具，系统启动时加载
func (repo *FunctionRepository) InitializeSystemTool(tools []dataobject.TTool) {
	if len(tools) == 0 {
		//没有基础工具,系统照样运行
		return
	}

	// 将基础工具解析到agent中
	repo.mu.Lock()
	defer repo.mu.Unlock()
	repo.initializeSystemTools(tools)
}

// GetFunctionBundle 每轮对话的所有的 function-calling 输入。
//
// skillDocs:prompt repo 已解析的 SKILL.md 文档(SKILL.md 的唯一读盘/解析入口的产物);
// 本层不读盘、不解析 YAML,只做派生(脚本绝对路径、LLM schema、技能级元数据)。
//
// 三份视图(schema / 强制名 / 脚本路径)全部出自同一次收集,
// 保证顺序与过滤口径完全一致:否则会出现"某个工具被过滤掉了、却仍在强制名单里"
// (策略层判为"强制工具不在可用集"→ 静默降级)。
func (repo *FunctionRepository) GetFunctionBundle(skillDocs []hCommon.SkillDocument) FunctionBundle {
	basicTools, slots := repo.collectToolsAndSkills(skillDocs)
	bundle := FunctionBundle{
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
		bundle.Scripts[t.Name] = t.Script //此时执行脚本是绝对路径
	}

	bundle.ForcedNames = pCommon.NormalizeStrings(bundle.ForcedNames) //规范化名称,去重等
	return bundle
}

func (repo *FunctionRepository) Destroy() {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	repo.systemTools = nil
}

// ===================================== private functions & methods ==================================

func (repo *FunctionRepository) initializeSystemTools(tools []dataobject.TTool) {
	for _, t := range tools {
		if t.ToolName == pCommon.BlankString || t.Script == pCommon.BlankString {
			continue
		}

		toolSchemaFile := resolveSystemFile(t.ToolName, t.InputSchema)
		schemaFp := pCommon.ComputeFileFingerprint(toolSchemaFile)
		systemTool, ok := repo.systemTools[t.ToolId]
		if ok {
			//库中已经存在
			if strings.EqualFold(schemaFp, systemTool.SchemaFingerprint) {
				//此时schema 文件没有被修改过
				continue
			}
		}

		//新增或修改
		tempSysTool := hCommon.SystemicTool{
			SchemaFileName:    toolSchemaFile,
			SchemaFingerprint: schemaFp,
		}

		if len(repo.systemTools) > 16 {
			//保护system tool 溢出
			repo.systemTools = make(map[string]hCommon.SystemicTool)
		}
		repo.systemTools[t.ToolId] = tempSysTool
	}
}

// collectToolsAndSkills 由已解析的技能文档收集工具声明与**技能级元数据**。
//
// 两份产物出自同一次遍历、同一份数据(prompt repo 缓存的文档),因此不需要任何 IO;
// 技能元数据(关键词/强制工具)与工具列表也不会错位。
func (repo *FunctionRepository) collectToolsAndSkills(skillDocs []hCommon.SkillDocument) ([]hCommon.BasicTool, []SkillSlot) {
	var disallowedTools []string
	var skillTools []hCommon.BasicTool
	var slots []SkillSlot

	for i := range skillDocs {
		// front matter 的工具声明 → 工具 schema + 脚本绝对路径(相对 SKILL.md 所在目录)
		skillTool, ok := hCommon.BuildSkillTool(skillDocs[i])
		if !ok {
			continue
		}

		skillTools = append(skillTools, skillTool.Tools...)
		disallowedTools = append(disallowedTools, skillTool.DisallowedTools...)
		slots = append(slots, buildSkillSlot(&skillTool))
	}

	// 此时无skill文件,仅返回基础工具
	repo.mu.Lock()
	baseTools := repo.getToolsFromSystemTool(disallowedTools) //若disallowedTools为空,则表示所有系统工具均可用
	repo.mu.Unlock()

	result := make([]hCommon.BasicTool, 0, len(baseTools)+len(skillTools))
	result = append(result, skillTools...)
	result = append(result, baseTools...)

	return result, slots
}

func (repo *FunctionRepository) getToolsFromSystemTool(disallowedList []string) []hCommon.BasicTool {
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

// buildSkillSlot 把一个已解析的技能转成技能级元数据(只取“技能自己的”属性)。
func buildSkillSlot(skillTool *hCommon.SkillTool) SkillSlot {
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

// ================
func resolveSystemFile(toolName, inputFile string) string {
	systemToolRoot := filepath.Join(pCommon.GetExecutableDir(), hCommon.AgentBootstrapDir, hCommon.SystemToolDirRoot) //
	basePath := filepath.Dir(inputFile)

	outputFile, _ := pCommon.ResolveFile(filepath.Join(systemToolRoot, toolName, basePath), inputFile)
	return outputFile
}
