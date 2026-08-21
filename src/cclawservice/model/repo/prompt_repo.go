/**
* Agent 启动文档与 Skill 元数据的文件仓库，对应每个agent
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
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v2"
	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/utility"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
)

// PromptRepository 负责从文件系统加载并缓存 prompt 相关的文档
type PromptRepository struct {
	mu sync.Mutex

	workspaceRoot string

	documents *pCommon.SafeList[dataobject.AgentDocument]
	skills    *pCommon.SafeList[dataobject.SkillDocument]

	// 加载时记录的文件指纹,用于判断文件是否发生变化
	loadedFingerprint string
}

// IPromptRepository 定义 prompt 文档仓库的对外能力
type IPromptRepository interface {
	LoadWorkspace(workspaceRoot string) error
	AddSkill(skillId, skillFile string) error
	ClearSkills()

	GetAllDocuments() []dataobject.AgentDocument
	GetAllSkills() []dataobject.SkillDocument

	Destroy()
}

var promptRepository = pCommon.GetSingleton(func() *PromptRepository {
	return &PromptRepository{
		workspaceRoot:     "",
		documents:         pCommon.NewSafeList[dataobject.AgentDocument](),
		skills:            pCommon.NewSafeList[dataobject.SkillDocument](),
		loadedFingerprint: "",
	}
}).Instance()

// GetPromptRepository 返回全局唯一的 prompt 仓库实例
func GetPromptRepository() IPromptRepository {
	return promptRepository
}

// LoadWorkspace 加载并缓存一个工作区的启动文档。只是加载agent的工作区
// 当工作区未变化(指纹相同)时直接复用缓存,避免重复读盘。
// 单个文档缺失/失败不阻断整个工作区的加载。
func (repo *PromptRepository) LoadWorkspace(workspaceRoot string) error {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if len(workspaceRoot) == 0 {
		return exception.ParameterEmptyErrorEx("prompt repo: workspace root is empty")
	}
	workspaceRoot = pCommon.FormatPath(workspaceRoot)

	repo.mu.Lock()
	defer repo.mu.Unlock()

	var fileNames []string
	for _, item := range cCommon.StartupDocFiles {
		fileNames = append(fileNames, item.Name)
	}

	if repo.workspaceRoot == workspaceRoot &&
		repo.loadedFingerprint == pCommon.ComputeFileFingerprint(workspaceRoot, fileNames) { //repo.computeFingerprintLocked(workspaceRoot) {
		return nil // 未变化,直接复用缓存
	}

	repo.documents.Clear()
	repo.skills.Clear()
	repo.workspaceRoot = workspaceRoot

	for _, item := range cCommon.StartupDocFiles {
		doc, ok, err := repo.readAgentDocumentLocked(item.Kind, item.Name, item.Required)
		if err != nil {
			logging.LogWarnF("prompt repo: load %s error:%s", item.Name, err.Error())
			continue
		}
		if ok {
			repo.documents.Append(*doc)
		}
	}

	repo.loadedFingerprint = pCommon.ComputeFileFingerprint(workspaceRoot, fileNames) //repo.computeFingerprintLocked(workspaceRoot)
	return nil
}

// AddSkill 加载并缓存一个 SKILL.md 文档(含元数据解析)。
// 文件缺失时仅告警不报错,保证 prompt 构建的可用性。
func (repo *PromptRepository) AddSkill(skillId, skillFile string) error {
	skillId = strings.TrimSpace(skillId)
	skillFile = strings.TrimSpace(skillFile)
	if len(skillId) == 0 || len(skillFile) == 0 {
		return exception.ParameterEmptyErrorEx("prompt repo: skill id or skill file is empty")
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()

	filePath, err := repo.resolveSkillPathLocked(skillFile)
	if err != nil {
		return err
	}
	if !pCommon.FileExists(filePath) {
		logging.LogWarnF("prompt repo: skill file not found:%s", filePath)
		return nil
	}

	data, size, err := pCommon.ReadFile(filePath, cCommon.MaxSkillDocSize)
	if err != nil {
		return err
	}

	frontMatter, content := utility.SplitFrontMatter(data)

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

	repo.skills.Append(*doc)
	return nil
}

// ClearSkills 清空已缓存的 skill 文档(每次组装 prompt 前调用,避免跨 agent 残留)
func (repo *PromptRepository) ClearSkills() {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	repo.skills.Clear()
}

// GetAllDocuments 返回全部启动文档的副本
func (repo *PromptRepository) GetAllDocuments() []dataobject.AgentDocument {
	out := make([]dataobject.AgentDocument, 0, repo.documents.Len())
	repo.documents.Range(func(_ int, item dataobject.AgentDocument) bool {
		out = append(out, item)
		return true
	})

	return out
}

// GetAllSkills 返回全部 skill 文档的副本
func (repo *PromptRepository) GetAllSkills() []dataobject.SkillDocument {
	out := make([]dataobject.SkillDocument, 0, repo.skills.Len())
	repo.skills.Range(func(_ int, item dataobject.SkillDocument) bool {
		out = append(out, item)
		return true
	})

	return out
}

// Destroy 释放仓库持有的全部缓存
func (repo *PromptRepository) Destroy() {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	repo.workspaceRoot = ""
	repo.loadedFingerprint = ""
	if repo.documents != nil {
		repo.documents.Clear()
	}
	if repo.skills != nil {
		repo.skills.Clear()
	}
}

// ======================================= private methods =====================================

// readAgentDocumentLocked 读取单个启动文档并解析 front matter。
//   - ok=true 且 doc.Missing=false:文档存在
//   - ok=true 且 doc.Missing=true:必需文件缺失,content 为空,由组装层注入 [MISSING] 标记
//   - ok=false:可选文件缺失,直接省略
func (repo *PromptRepository) readAgentDocumentLocked(kind dataobject.AgentDocumentKind, fileName string, required bool) (*dataobject.AgentDocument, bool, error) {
	filePath, err := repo.resolveStartupPathLocked(fileName)
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

	data, size, err := pCommon.ReadFile(filePath, cCommon.MaxAgentDocSize)
	if err != nil {
		return nil, false, err
	}

	frontMatter, content := utility.SplitFrontMatter(data)

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

// resolveStartupPathLocked 校验并解析启动文档路径:仅允许白名单内的纯文件名,禁止目录穿越。
func (repo *PromptRepository) resolveStartupPathLocked(fileName string) (string, error) {
	fileName = strings.TrimSpace(fileName)
	if len(fileName) == 0 {
		return "", exception.ParameterEmptyErrorEx("prompt repo: document file name is empty")
	}

	// 只允许纯文件名:不能包含路径分隔符、"." 或 ".."
	if filepath.Base(fileName) != fileName || fileName == "." || fileName == ".." {
		return "", exception.FilePathErrorEx("prompt repo: illegal document file name: " + fileName)
	}

	return pCommon.SafeJoin(repo.workspaceRoot, filepath.Join(repo.workspaceRoot, fileName)) //repo.safeJoinLocked(repo.workspaceRoot, filepath.Join(repo.workspaceRoot, fileName))
}

// resolveSkillPathLocked 解析 skill 文件路径:相对路径相对于工作区根,绝对路径直接使用。
// skill 文件来自数据库配置(TSkill.SkillFile,管理员维护),允许位于工作区之外的共享 skill 目录,
// 因此不做 workspace 包含性校验;安全约束聚焦于:文件名必须为 SKILL.md + 单文件大小上限。
func (repo *PromptRepository) resolveSkillPathLocked(skillFile string) (string, error) {
	skillFile = strings.TrimSpace(skillFile)
	if len(skillFile) == 0 {
		return "", exception.ParameterEmptyErrorEx("prompt repo: skill file is empty")
	}
	if filepath.Base(skillFile) != cCommon.SkillDocFileName {
		return "", exception.FilePathErrorEx("prompt repo: skill file must be SKILL.md: " + skillFile)
	}

	filePath := skillFile
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(repo.workspaceRoot, filePath)
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return "", exception.FilePathErrorEx("prompt repo: resolve skill path error: " + err.Error())
	}

	return filepath.Clean(absPath), nil
}
