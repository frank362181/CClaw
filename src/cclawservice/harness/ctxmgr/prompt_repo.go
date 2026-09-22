/**
* Agent 启动文档与 Skill 元数据的文件仓库，对应每个agent
* 本系统支持多个agent在线
*
* 职责:
*   1. 从 agent 的工作区根目录读取 AGENTS.md(agent.md)/SOUL.md(soul.md)/TOOLS.md(tools.md)
*   2. 读取 SKILL.md 并解析其 YAML Front Matter(元数据与正文)——
*      **SKILL.md 的唯一读盘 + 唯一 YAML 解析入口**(工具层只消费 LoadSkills 的产物)
*   3. 内存缓存 + 指纹(文件大小/修改时间)判定,避免重复读盘
*      (启动文档按工作区指纹、技能文档按单文件指纹;技能文档与 agent 无关,全仓库只缓存一份)
*
* 与 AgentRepository 保持一致:common.GetSingleton 单例 + common.SafeList 线程安全缓存。
**/

package ctxmgr

import (
	"strings"
	"sync"

	"gopkg.in/yaml.v2"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/observatory"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
)

// 表示一个agent的prompt DOCUMENT缓存
type agentPrompt struct {
	agentId            string
	agentBootstrapRoot string

	documents *pCommon.SafeList[hCommon.AgentDocument]

	// 加载时记录的文件指纹,用于判断文件是否发生变化
	loadedFingerprint string
}

// maxSkillDocCacheEntries 技能文档缓存条目上限,超过则整体清空(防陈旧条目无限累积)
const maxSkillDocCacheEntries = 256

// PromptRepository 负责从文件系统加载并缓存 prompt 相关的文档
//
// 并发约定:mu 只保护 map/列表的读改;磁盘 IO(路径解析/stat/读文件/YAML 解析)**一律在锁外**,
// 只把“取快照”与“回写新条目”两段极短操作放进锁内 —— 否则一个慢盘或大文件会阻塞所有 agent。
type PromptRepository struct {
	mu sync.RWMutex

	systemTools map[string]hCommon.SystemToolSchema

	promptMap map[string]*agentPrompt

	// skillCache 已解析的 SKILL.md 文档缓存:key = SKILL.md 绝对路径。
	skillCache map[string]hCommon.SkillDocument
}

// IPromptRepository 定义 prompt 文档仓库的对外能力
type IPromptRepository interface {
	LoadAgentBootstrap(agent dataobject.TAgent) error

	// LoadSystemTools 加载系统工具
	LoadSystemTools(tools []dataobject.TTool) ([]hCommon.SystemToolSchema, observatory.SkillLoadReport)

	// LoadSkills 是 SKILL.md 的唯一读盘 + YAML 解析入口(按文件指纹缓存)。
	LoadSkills(skillFiles []string) ([]hCommon.SkillDocument, observatory.SkillLoadReport)

	GetAllDocuments(agentId string) []hCommon.AgentDocument

	Destroy()
}

var promptRepository = pCommon.GetSingleton(func() *PromptRepository {
	return &PromptRepository{
		promptMap:  make(map[string]*agentPrompt),
		skillCache: make(map[string]hCommon.SkillDocument),
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
	agentBootstrapRoot, ok := hCommon.BuildAgentBootstrapRoot(agent)
	if !ok {
		return exception.ParameterEmptyErrorEx("prompt repo: workspace root is unavailable")
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()

	var fileNames []string
	for _, item := range hCommon.StartupDocFiles {
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
		prompt.agentBootstrapRoot = agentBootstrapRoot
	} else {
		//初次加载
		prompt = &agentPrompt{
			agentId:            agent.AgentId,
			agentBootstrapRoot: agentBootstrapRoot,
			documents:          pCommon.NewSafeList[hCommon.AgentDocument](),
			loadedFingerprint:  "",
		}
	}

	for _, item := range hCommon.StartupDocFiles {
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

// LoadSystemTools 将系统工具的schema 加载进来
func (repo *PromptRepository) LoadSystemTools(tools []dataobject.TTool) ([]hCommon.SystemToolSchema, observatory.SkillLoadReport) {
	observer := observatory.NewObserver(len(tools))
	if len(tools) == 0 {
		return nil, observer
	}

	for _, tool := range tools {
		if len(strings.TrimSpace(tool.ToolName)) == 0 {
			observer.Skip(tool.ToolName, "empty tool name")
			continue
		}
	}

	return nil, observer
}

// LoadSkills 解析并缓存一组 SKILL.md,返回本轮可用的技能文档(按入参顺序去重)与加载统计。
//
// 这是 SKILL.md 的**唯一读盘 + 唯一 YAML 解析入口**:工具层只消费返回文档的 FrontMatter,
// prompt 层只消费目录与正文 —— 都不再自己读文件。
func (repo *PromptRepository) LoadSkills(skillFiles []string) ([]hCommon.SkillDocument, observatory.SkillLoadReport) {
	report := observatory.NewObserver(len(skillFiles))
	if len(skillFiles) == 0 {
		return nil, report
	}

	// ---- 1) 归一化(锁外):技能目录配置 → SKILL.md 绝对路径,并去重 ----
	paths := make([]string, 0, len(skillFiles))
	seen := make(map[string]struct{}, len(skillFiles))
	for _, skillFile := range skillFiles {
		skillFile = strings.TrimSpace(skillFile)
		if len(skillFile) == 0 {
			report.Skip(skillFile, "empty skill dir")
			continue
		}

		// 越界校验(相对 skills 根解析 + 真实路径包含性)在 common 里单点收敛
		filePath, err := hCommon.ResolveSkillDocPath(skillFile)
		if err != nil {
			report.Skip(skillFile, err.Error())
			continue
		}
		if _, dup := seen[filePath]; dup {
			report.Duplicated++ // 同一技能被重复绑定:只保留一份(否则工具与目录项都会重复)
			continue
		}
		seen[filePath] = struct{}{}
		paths = append(paths, filePath)
	}

	// ---- 2) 取缓存快照(读锁,极短) ----
	cached := make(map[string]hCommon.SkillDocument, len(paths))
	repo.mu.RLock()
	for _, filePath := range paths {
		if doc, hit := repo.skillCache[filePath]; hit {
			cached[filePath] = doc
		}
	}
	repo.mu.RUnlock()

	// ---- 3) 命中复用 / 未命中读盘 + 解析(锁外,最慢的一段) ----
	out := make([]hCommon.SkillDocument, 0, len(paths))
	fresh := make([]hCommon.SkillDocument, 0, len(paths))
	for _, filePath := range paths {
		// 指纹必须在**读之前**取:这样落进缓存的指纹永远不新于正文 —— 与写入竞态时最坏是
		// “下一轮多读一次”,绝不会把旧正文挂在新指纹下(顺序反过来就会变成“改了也不重读”)。
		fingerprint := pCommon.ComputeFileFingerprint(filePath)
		if len(fingerprint) == 0 {
			report.Skip(filePath, "file not found")
			continue
		}

		// 指纹命中:直接复用已解析文档(正文 + front matter),零读盘零解析
		if doc, hit := cached[filePath]; hit && doc.Fingerprint == fingerprint {
			out = append(out, doc)
			report.Cached++
			continue
		}

		doc, err := hCommon.ReadSkillDocument(filePath, fingerprint)
		if err != nil {
			report.Skip(filePath, err.Error()) // 超大/不可读:整份跳过(不截断)
			continue
		}

		fresh = append(fresh, *doc)
		out = append(out, *doc)
		report.Reloaded++
	}

	// ---- 4) 回写新条目(写锁,极短) ----
	if len(fresh) > 0 {
		repo.mu.Lock()
		for i := range fresh {
			repo.cacheSkillLocked(fresh[i])
		}
		repo.mu.Unlock()
	}

	report.Loaded = len(out)
	return out, report
}

// GetAllDocuments 返回全部启动文档的副本
func (repo *PromptRepository) GetAllDocuments(agentId string) []hCommon.AgentDocument {
	agentId = strings.TrimSpace(agentId)
	if len(agentId) == 0 {
		return nil
	}

	// 读锁:与 LoadAgentBootstrap 的“清空 + 逐条重填”互斥,否则可能读到半空的文档列表
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	prompt := repo.promptMap[agentId]
	if prompt == nil {
		return nil
	}

	documents := prompt.documents
	out := make([]hCommon.AgentDocument, 0, documents.Len())
	documents.Range(func(_ int, item hCommon.AgentDocument) bool {
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
	}

	repo.skillCache = make(map[string]hCommon.SkillDocument)
}

// ======================================= private methods =====================================

// readAgentDocumentLocked 读取单个启动文档并解析 front matter。
//   - ok=true 且 doc.Missing=false:文档存在
//   - ok=true 且 doc.Missing=true:必需文件缺失,content 为空,由组装层注入 [MISSING] 标记
//   - ok=false:可选文件缺失,直接省略
func (repo *PromptRepository) readAgentDocumentLocked(kind hCommon.AgentDocumentKind, workspaceRoot, fileName string, required bool) (*hCommon.AgentDocument, bool, error) {
	filePath, err := hCommon.ResolveStartupPath(workspaceRoot, fileName)
	if err != nil {
		return nil, false, err
	}

	base := &hCommon.AgentDocument{
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

// cacheSkillLocked 写入技能解析缓存(调用方需持有 repo.mu)
func (repo *PromptRepository) cacheSkillLocked(doc hCommon.SkillDocument) {
	if len(repo.skillCache) >= maxSkillDocCacheEntries {
		repo.skillCache = make(map[string]hCommon.SkillDocument) // 满则整体重置,防陈旧条目无限累积
	}
	repo.skillCache[doc.SkillFile] = doc
}
