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
	"path/filepath"
	"strings"
	"sync"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/llmproto"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/platformutility/common"
	pCommon "my.favorite/platformutility/common"
)

type FunctionRepository struct {
	mu sync.RWMutex

	systemTools []hCommon.SystemicTool //基础工具

	skillTools map[string]hCommon.SkillTool //skill工具,与一个文件skill.md文件挂钩

	// 用来维护skill中的took
	capacity int
	hits     int
	misses   int
}

// FunctionBundle 用来构建function-calling计划使用
type FunctionBundle struct {
	Tools       []llmproto.Tool   // 写一层schema,GetLLMTools的产出
	ForcedNames []string          // 声明了force-call的工具名
	Scripts     map[string]string // 工具名 --> 脚本路径
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
	basicTools := repo.collectBasicToolsWithScripts(skillRoot, skillFiles)
	bundle := FunctionBundle{
		Tools:       make([]llmproto.Tool, 0, len(basicTools)),
		ForcedNames: make([]string, 0, len(basicTools)),
		Scripts:     make(map[string]string),
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

// ===================================== private functions & methods ==================================
func (repo *FunctionRepository) collectBasicTools(skillRoot string, skillFiles []string) []hCommon.BasicTool {
	tools := repo.collectBasicToolsWithScripts(skillRoot, skillFiles)
	return tools
}

// collectBasicToolsWithScripts 收集本轮可用的工具声明,并**同时**把声明了 script 的技能
// 工具解析成绝对路径(工具名 → 脚本绝对路径)。
func (repo *FunctionRepository) collectBasicToolsWithScripts(skillRoot string, skillFiles []string) []hCommon.BasicTool {
	var disallowedTools []string
	var skillTools []hCommon.BasicTool

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

	return result
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
