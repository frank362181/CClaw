/**
* Agent 启动文档与 Skill 元数据的文件仓库，对应每个agent
* 本系统支持多个agent在线
*
* 职责:
*   1. 从 agent 的工作区根目录读取 AGENTS.md(agent.md)/SOUL.md(soul.md)/TOOLS.md(tools.md)
*   2. 读取 SKILL.md 并解析其 YAML Front Matter(元数据)
*   3. 内存缓存 + 指纹(文件大小/修改时间)判定,避免重复读盘
*
* 与 AgentRepository 保持一致:common.GetSingleton 单例 + common.SafeList 线程安全缓存。
**/

package repo

import (
	"strings"
	"sync"

	"gopkg.in/yaml.v2"
	cCommon "my.favorite/cclawservice/common"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
)

// 表示一个agent的prompt DOCUMENT缓存
type agentPrompt struct {
	agentId            string
	agentBootstrapRoot string

	documents *pCommon.SafeList[dataobject.AgentDocument]
	skills    *pCommon.SafeList[dataobject.SkillDocument]

	// 加载时记录的文件指纹,用于判断文件是否发生变化
	loadedFingerprint string
}

// PromptRepository 负责从文件系统加载并缓存 prompt 相关的文档
type PromptRepository struct {
	mu sync.Mutex

	promptMap map[string]*agentPrompt
}

// IPromptRepository 定义 prompt 文档仓库的对外能力
type IPromptRepository interface {
	LoadAgentBootstrap(agent dataobject.TAgent) error

	AddSkill(agentId, skillId, skillFile string) error
	ClearSkills(agentId string)

	GetAllDocuments(agentId string) []dataobject.AgentDocument
	GetAllSkills(agentId string) []dataobject.SkillDocument

	Destroy()
}

var promptRepository = pCommon.GetSingleton(func() *PromptRepository {
	return &PromptRepository{
		promptMap: make(map[string]*agentPrompt),
	}
}).Instance()

// GetPromptRepository 返回全局唯一的 prompt 仓库实例
func GetPromptRepository() IPromptRepository {
	return promptRepository
}

// LoadAgentBootstrap 加载并缓存一个工作区的启动文档。只是加载agent的工作区
// 当工作区未变化(指纹相同)时直接复用缓存,避免重复读盘。
// 单个文档缺失/失败不阻断整个工作区的加载。
func (repo *PromptRepository) LoadAgentBootstrap(agent dataobject.TAgent) error {
	agentBootstrapRoot, ok := cCommon.BuildAgentBootstrapRoot(agent)
	if !ok {
		return exception.ParameterEmptyErrorEx("prompt repo: workspace root is unavailable")
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()

	var fileNames []string
	for _, item := range cCommon.StartupDocFiles {
		fileNames = append(fileNames, item.Name)
	}

	prompt := repo.promptMap[agent.AgentId]
	if prompt != nil {
		// 判断是否修改过 *.md 文件
		if prompt.agentBootstrapRoot == agentBootstrapRoot &&
			prompt.loadedFingerprint == pCommon.ComputeFilesFingerprint(agentBootstrapRoot, fileNames) {
			return nil // 未变化,直接复用缓存
		}

		// *.md文件已经被修改过
		prompt.documents.Clear()
		prompt.skills.Clear()
		prompt.agentBootstrapRoot = agentBootstrapRoot
	} else {
		//初次加载
		prompt = &agentPrompt{
			agentId:            agent.AgentId,
			agentBootstrapRoot: agentBootstrapRoot,
			documents:          pCommon.NewSafeList[dataobject.AgentDocument](),
			skills:             pCommon.NewSafeList[dataobject.SkillDocument](),
			loadedFingerprint:  "",
		}
	}

	for _, item := range cCommon.StartupDocFiles {
		doc, ok, err := repo.readAgentDocumentLocked(item.Kind, agentBootstrapRoot, item.Name, item.Required)
		if err != nil {
			logging.LogWarnF("prompt repo: load %s error:%s", item.Name, err.Error())
			continue
		}
		if ok {
			prompt.documents.Append(*doc)
		}
	}

	prompt.loadedFingerprint = pCommon.ComputeFilesFingerprint(agentBootstrapRoot, fileNames)
	repo.promptMap[agent.AgentId] = prompt
	return nil
}

// AddSkill 加载并缓存一个 SKILL.md 文档(含元数据解析)。
// 文件缺失时仅告警不报错,保证 prompt 构建的可用性。
func (repo *PromptRepository) AddSkill(agentId, skillId, skillFile string) error {
	agentId = strings.TrimSpace(agentId)
	skillId = strings.TrimSpace(skillId)
	skillFile = strings.TrimSpace(skillFile)
	if len(agentId) == 0 || len(skillId) == 0 || len(skillFile) == 0 {
		return exception.ParameterEmptyErrorEx("prompt repo: skill id or skill file is empty")
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()

	agentPrompt := repo.promptMap[agentId]
	if agentPrompt == nil {
		return exception.UnMatchErrorEx2("prompt repo: agent id not found")
	}

	// 技能目录配置 → SKILL.md 绝对路径(相对 skills 根解析 + 越界校验都在 common 里单点收敛)
	filePath, err := cCommon.ResolveSkillDocPath(skillFile)
	if err != nil {
		return err
	}
	if !pCommon.FileExists(filePath) {
		logging.LogWarnF("prompt repo: skill file not found:%s", filePath)
		return nil
	}

	data, size, err := pCommon.ReadFile(filePath, hCommon.MaxSkillDocSize)
	if err != nil {
		return err
	}

	frontMatter, content := hCommon.SplitFrontMatter(data)

	doc := &dataobject.SkillDocument{
		SkillId:   skillId,
		SkillFile: filePath,
		Content:   content,
		ByteSize:  size,
	}
	if len(frontMatter) > 0 {
		if err := yaml.Unmarshal([]byte(frontMatter), &doc.FrontMatter); err != nil {
			logging.LogWarnF("prompt repo: parse skill %s front matter error:%s", filePath, err.Error())
		}
	}

	repo.promptMap[agentId].skills.Append(*doc)
	return nil
}

// ClearSkills 清空已缓存的 skill 文档(每次组装 prompt 前调用,避免跨 agent 残留)
func (repo *PromptRepository) ClearSkills(agentId string) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	prompt := repo.promptMap[agentId]
	if prompt == nil {
		return
	}
	prompt.skills.Clear()
}

// GetAllDocuments 返回全部启动文档的副本
func (repo *PromptRepository) GetAllDocuments(agentId string) []dataobject.AgentDocument {
	agentId = strings.TrimSpace(agentId)
	if len(agentId) == 0 {
		return nil
	}
	prompt := repo.promptMap[agentId]
	if prompt == nil {
		return nil
	}

	documents := prompt.documents
	out := make([]dataobject.AgentDocument, 0, documents.Len())
	documents.Range(func(_ int, item dataobject.AgentDocument) bool {
		out = append(out, item)
		return true
	})

	return out
}

// GetAllSkills 返回全部 skill 文档的副本
func (repo *PromptRepository) GetAllSkills(agentId string) []dataobject.SkillDocument {
	agentId = strings.TrimSpace(agentId)
	if len(agentId) == 0 {
		return nil
	}

	prompt := repo.promptMap[agentId]
	if prompt == nil {
		return nil
	}

	skills := prompt.skills
	out := make([]dataobject.SkillDocument, 0, skills.Len())
	skills.Range(func(_ int, item dataobject.SkillDocument) bool {
		out = append(out, item)
		return true
	})

	return out
}

// Destroy 释放仓库持有的全部缓存
func (repo *PromptRepository) Destroy() {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	for _, item := range repo.promptMap {
		if item == nil {
			continue
		}

		item.agentBootstrapRoot = ""
		item.loadedFingerprint = ""
		if item.documents != nil {
			item.documents.Clear()
		}

		if item.skills != nil {
			item.skills.Clear()
		}
	}
}

// ======================================= private methods =====================================

// readAgentDocumentLocked 读取单个启动文档并解析 front matter。
//   - ok=true 且 doc.Missing=false:文档存在
//   - ok=true 且 doc.Missing=true:必需文件缺失,content 为空,由组装层注入 [MISSING] 标记
//   - ok=false:可选文件缺失,直接省略
func (repo *PromptRepository) readAgentDocumentLocked(kind dataobject.AgentDocumentKind, workspaceRoot, fileName string, required bool) (*dataobject.AgentDocument, bool, error) {
	filePath, err := cCommon.ResolveStartupPath(workspaceRoot, fileName)
	if err != nil {
		return nil, false, err
	}

	base := &dataobject.AgentDocument{
		Kind:     kind,
		FileName: fileName,
		Required: required,
		Missing:  true,
	}

	if !pCommon.FileExists(filePath) {
		if required {
			return base, true, nil // 必需文件缺失:保留占位,供 [MISSING] 标记
		}
		return nil, false, nil // 可选文件缺失:省略
	}

	data, size, err := pCommon.ReadFile(filePath, hCommon.MaxAgentDocSize)
	if err != nil {
		return nil, false, err
	}

	frontMatter, content := hCommon.SplitFrontMatter(data)

	base.Content = content
	base.ByteSize = size
	base.Missing = false
	if len(frontMatter) > 0 {
		if err := yaml.Unmarshal([]byte(frontMatter), &base.FrontMatter); err != nil {
			logging.LogWarnF("prompt repo: parse %s front matter error:%s", fileName, err.Error())
		}
	}

	return base, true, nil
}
