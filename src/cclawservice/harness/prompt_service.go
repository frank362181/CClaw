/**
* System Prompt 组装服务
*
* 职责:基于 agent 的工作区启动文档(AGENTS.md/SOUL.md/IDENTITY.md/USER.md/TOOLS.md/
*      BOOTSTRAP.md/MEMORY.md)与 SKILL.md 元数据,按固定顺序组装 system prompt。
*
*   - 固定注入顺序:AGENTS -> SOUL -> IDENTITY -> USER -> TOOLS -> BOOTSTRAP -> MEMORY
*   - 必需文件(AGENTS.md)缺失时注入 [MISSING] 标记;可选文件缺失/空白则省略
*   - 按会话范围(正常/子代理/定时/心跳)过滤注入的文件,并据此控制 MEMORY.md 等隐私文件
*   - 预算:每文件 20K、USER.md 4K、总量 60K;超限截断并记录
*   - skill 只注入元数据目录(名称+版本+描述),正文按需加载;目录有独立预算
*
* 与 LoginService 保持一致:组合 baseService.BaseService + 工厂方法 GetPromptService()。
**/

package harness

import (
	"path/filepath"
	"sort"
	"strings"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/model/repo"
	common2 "my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"
	baseService "my.favorite/platformutility/webbase/middleware/service"
)

// PromptService contains the system prompt assembly rules.
type PromptService struct {
	baseService.BaseService

	promptRepo repo.IPromptRepository
}

// GetPromptService builds a fresh service graph instead of mutating a package singleton.
func GetPromptService() *PromptService {
	return &PromptService{
		BaseService: baseService.BaseService{
			Name:        "promptService",
			Description: "build system prompt from agent startup documents and skill metadata",
		},
		promptRepo: repo.GetPromptRepository(),
	}
}

// BuildSystemPrompt 以正常会话范围组装 system prompt(兼容旧调用)。
//   - agent:       agent 对象
//   - skillFiles:    需要载入元数据的 SKILL.md 文件列表(可为空)
func (service *PromptService) BuildSystemPrompt(agent dataobject.TAgent, skillFiles []string) (*dataobject.SystemPrompt, error) {
	return service.BuildSystemPromptEx(agent, skillFiles, dataobject.BootstrapScopeNormal)
}

// BuildSystemPromptEx 按指定会话范围组装 system prompt。
// 使用时机与 OpenClaw 一致:新会话首轮注入完整启动文档,后续每轮按指纹复用缓存
// (文件未变零读盘),从而让长会话能看到工作区文件的编辑。
func (service *PromptService) BuildSystemPromptEx(agent dataobject.TAgent, skillFiles []string, scope dataobject.BootstrapScope) (*dataobject.SystemPrompt, error) {
	workspaceRoot, _ := cCommon.BuildAgentBootstrapRoot(agent)

	// 1. 加载并缓存 agent 启动文档(指纹未变化时直接命中缓存)
	if err := service.promptRepo.LoadAgentBootstrap(agent); err != nil {
		return nil, err
	}

	// 2. 加载 skill 元数据(只解析 front matter,不加载正文)
	service.promptRepo.ClearSkills(agent.AgentId)
	for _, skillFile := range skillFiles {
		skillFile = strings.TrimSpace(skillFile)
		if len(skillFile) == 0 {
			continue
		}

		skillId := filepath.Base(skillFile)
		if err := service.promptRepo.AddSkill(agent.AgentId, skillId, skillFile); err != nil {
			logging.LogWarnF("prompt service: load skill %s error:%s", skillFile, err.Error())
		}
	}

	// 3. 组装 system prompt
	return service.assembleSystemPrompt(agent, workspaceRoot, scope)
}

// assembleSystemPrompt 按规范顺序组装,应用会话范围过滤、缺失标记与预算控制。
func (service *PromptService) assembleSystemPrompt(agent dataobject.TAgent, workspaceRoot string, scope dataobject.BootstrapScope) (*dataobject.SystemPrompt, error) {
	documents := service.promptRepo.GetAllDocuments(agent.AgentId) // 已按规范顺序加载
	skills := service.promptRepo.GetAllSkills(agent.AgentId)

	var missing, truncated []string
	sections := make([]dataobject.SystemPromptSection, 0, len(documents)+1)

	// ---- 启动文档(顺序:AGENTS -> SOUL -> IDENTITY -> USER -> TOOLS -> BOOTSTRAP -> MEMORY) ----
	for i := range documents {
		doc := &documents[i]
		if !isKindAllowedInScope(doc.Kind, scope) {
			continue
		}

		// 必需文件缺失:注入 [MISSING] 标记
		if doc.Missing {
			sections = append(sections, dataobject.SystemPromptSection{
				Title:   doc.FileName,
				Content: cCommon.MissingFileMarker + " Expected at: " + filepath.Join(workspaceRoot, doc.FileName),
			})
			missing = append(missing, doc.FileName)
			continue
		}

		content := strings.TrimSpace(doc.Content)
		if len(content) == 0 {
			continue // 空白文件跳过
		}

		// 单文件预算:USER.md 独立 4K,其余 20K
		budget := cCommon.MaxBootstrapFileChars
		if doc.Kind == dataobject.AgentDocUser {
			budget = cCommon.MaxUserDocChars
		}
		if runeLen := len([]rune(content)); runeLen > budget {
			content = common2.TruncateRunes(content, budget, common2.BlankString)
			truncated = append(truncated, doc.FileName)
		}

		sections = append(sections, dataobject.SystemPromptSection{
			Title:   doc.FileName,
			Content: content,
		})
	}

	// ---- skill 元数据目录(仅元数据,正文按需加载) ----
	skillCatalog, skillsTruncated := renderSkillsCatalog(skills)
	if len(skillCatalog) > 0 {
		sections = append(sections, dataobject.SystemPromptSection{
			Title:   "Available Skills",
			Content: skillCatalog,
		})
		if skillsTruncated {
			truncated = append(truncated, "Available Skills")
		}
	}

	// ---- 总预算控制:优先保留高优先级文件,超限从尾部压缩 ----
	sections, truncated = enforceTotalBudget(sections, agent.BootstrapTotalChars, truncated)

	// ---- 截断提示(OpenClaw: "## Bootstrap Context Notice") ----
	if len(truncated) > 0 {
		notice := "⚠️ Some bootstrap files were truncated to stay within the prompt budget: " +
			strings.Join(truncated, ", ") + "."
		sections = append(sections, dataobject.SystemPromptSection{
			Title:   "Bootstrap Context Notice",
			Content: notice,
		})
	}

	// ---- 拼接最终 prompt ----
	var sb strings.Builder
	for i := range sections {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		if len(sections[i].Title) > 0 {
			sb.WriteString("## ")
			sb.WriteString(sections[i].Title)
			sb.WriteString("\n\n")
		}
		sb.WriteString(sections[i].Content)
	}

	// ---- 输出语言要求(面向中文用户的统一约定;追加在提示词最末,始终生效、不受预算截断) ----
	sb.WriteString("\n\n## 输出语言\n\n")
	sb.WriteString("你面向中文用户。所有呈现给用户的输出一律使用简体中文:包括正式回复、思考/推理过程、" +
		"进度说明与工具调用说明;除非用户明确要求使用其他语言。")

	return &dataobject.SystemPrompt{
		AgentId:   agent.AgentId,
		Workspace: workspaceRoot,
		Scope:     scope,
		Sections:  sections,
		Prompt:    sb.String(),
		Length:    sb.Len(),
		Missing:   missing,
		Truncated: truncated,
	}, nil
}

// isKindAllowedInScope 按会话范围过滤启动文档(参考 OpenClaw filterBootstrapFilesForSession)。
//   - normal:   注入全部(含 MEMORY.md)
//   - subagent: 仅 AGENTS.md(不继承父 agent 的人格/用户/记忆)
//   - cron:     AGENTS/SOUL/IDENTITY/USER,不含 MEMORY.md
//   - heartbeat: 不注入启动文件
func isKindAllowedInScope(kind dataobject.AgentDocumentKind, scope dataobject.BootstrapScope) bool {
	switch scope {
	case dataobject.BootstrapScopeSubagent:
		return kind == dataobject.AgentDocAgent
	case dataobject.BootstrapScopeCron:
		switch kind {
		case dataobject.AgentDocAgent, dataobject.AgentDocSoul,
			dataobject.AgentDocIdentity, dataobject.AgentDocUser:
			return true
		}
		return false
	case dataobject.BootstrapScopeHeartbeat:
		return false
	default:
		return true
	}
}

// enforceTotalBudget 对全部注入内容施加总预算:从前向后保留高优先级文件,
// 超限时从尾部截断/丢弃,并返回被截断的文件清单。
func enforceTotalBudget(sections []dataobject.SystemPromptSection, cap int, truncated []string) ([]dataobject.SystemPromptSection, []string) {
	if cap <= 0 {
		return sections, truncated
	}

	total := 0
	for i := range sections {
		total += len([]rune(sections[i].Content))
	}
	if total <= cap {
		return sections, truncated
	}

	budget := cap
	kept := make([]dataobject.SystemPromptSection, 0, len(sections))
	for i := range sections {
		runeLen := len([]rune(sections[i].Content))
		if runeLen <= budget {
			budget -= runeLen
			kept = append(kept, sections[i])
			continue
		}

		// 该文件超出剩余预算:优先截断;预算耗尽则丢弃
		if budget > 0 {
			kept = append(kept, dataobject.SystemPromptSection{
				Title:   sections[i].Title,
				Content: common2.TruncateRunes(sections[i].Content, budget, common2.BlankString),
			})
			truncated = append(truncated, sections[i].Title)
			budget = 0
		} else {
			truncated = append(truncated, sections[i].Title)
		}
	}

	return kept, truncated
}

// renderSkillsCatalog 渲染 skill 元数据目录(参考 OpenClaw formatSkillsForPromptBounded):
//   - 仅输出元数据(名称 + 版本 + 描述),按名称排序,受数量与字符预算约束
//   - 优先完整描述;超出预算则先缩短描述,再逐条丢弃尾部 skill
//   - 返回 (目录文本, 是否被截断)
func renderSkillsCatalog(skills []dataobject.SkillDocument) (string, bool) {
	// 过滤空元数据
	catalog := make([]dataobject.SkillDocument, 0, len(skills))
	for i := range skills {
		meta := &skills[i].FrontMatter
		if len(meta.Name) == 0 && len(meta.Description) == 0 {
			continue
		}
		catalog = append(catalog, skills[i])
	}
	if len(catalog) == 0 {
		return "", false
	}

	// 按名称排序(OpenClaw localeCompare "en")
	sort.Slice(catalog, func(i, j int) bool {
		return strings.ToLower(catalog[i].FrontMatter.Name) < strings.ToLower(catalog[j].FrontMatter.Name)
	})

	// 数量上限
	if len(catalog) > cCommon.MaxSkillsInPrompt {
		catalog = catalog[:cCommon.MaxSkillsInPrompt]
	}

	render := func(list []dataobject.SkillDocument, descMax int) string {
		var sb strings.Builder
		for i := range list {
			meta := &list[i].FrontMatter
			sb.WriteString("- **")
			sb.WriteString(meta.Name)
			sb.WriteString("**")
			if len(meta.Version) > 0 {
				sb.WriteString(" (")
				sb.WriteString(meta.Version)
				sb.WriteString(")")
			}
			desc := meta.Description
			if descMax > 0 {
				desc = common2.TruncateRunes(desc, descMax, common2.BlankString)
			}
			if len(desc) > 0 {
				sb.WriteString(": ")
				sb.WriteString(desc)
			}
			sb.WriteString("\n")
		}
		return sb.String()
	}

	const minDesc = 4
	const maxDesc = 120

	// 完整描述若可容纳,直接使用
	if full := render(catalog, 0); len(full) <= cCommon.MaxSkillsPromptChars {
		return full, false
	}

	// 二分查找能容纳全部 skill 的最大描述长度
	fits := func(descMax int) bool {
		return len(render(catalog, descMax)) <= cCommon.MaxSkillsPromptChars
	}

	lo, hi := minDesc, maxDesc
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if fits(mid) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}

	if fits(lo) {
		return render(catalog, lo), true
	}

	// 最短描述也放不下:逐条丢弃尾部 skill 直到满足预算
	keep := make([]dataobject.SkillDocument, 0, len(catalog))
	for i := range catalog {
		candidate := append(keep, catalog[i])
		if len(render(candidate, lo)) > cCommon.MaxSkillsPromptChars {
			break
		}
		keep = candidate
	}
	if len(keep) == 0 {
		return "", true
	}
	return render(keep, lo), true
}
