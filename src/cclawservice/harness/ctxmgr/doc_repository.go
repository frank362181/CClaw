/**
* Agent对应的所有的启动文件,SKILLS.md,系统工具对应的.json等文件的仓库
* 本系统支持多个agent在线
*
* 职责:
*   1. 从 agent 的工作区根目录读取 AGENTS.md(agent.md)/SOUL.md(soul.md)/TOOLS.md(tools.md)
*   2. 读取 SKILL.md 并解析其 YAML Front Matter(元数据与正文)——
*      **SKILL.md 的唯一读盘 + 唯一 YAML 解析入口**(工具层只消费 LoadSkills 的产物)
*	3. 读取系统工具对应的.json文件
*   4. 内存缓存 + 指纹(文件大小/修改时间)判定,避免重复读盘
*      (启动文档按工作区指纹、技能文档按单文件指纹;技能文档与 agent 无关,全仓库只缓存一份)
*
**/

package ctxmgr

import (
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v2"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/observatory"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
)

// 表示一个agent对应的所有 系统DOCUMENT 缓存,一般表示启动文件
type agentSystemDocument struct {
	agentId            string
	agentBootstrapRoot string
	documents          *pCommon.SafeList[hCommon.AgentDocument]
	loadedFingerprint  string // 加载时记录的文件指纹,用于判断文件是否发生变化
}

// maxSkillDocCacheEntries 技能文档缓存条目上限,超过则整体清空(防陈旧条目无限累积)
const maxSkillDocCacheEntries = 256

// AgentDocumentRepository 负责从文件系统加载并缓存 prompt 相关的文档
//
// 并发约定:mu 只保护 map/列表的读改;磁盘 IO(路径解析/stat/读文件/YAML 解析)**一律在锁外**,
// 只把“取快照”与“回写新条目”两段极短操作放进锁内 —— 否则一个慢盘或大文件会阻塞所有 agent。
type AgentDocumentRepository struct {
	mu sync.RWMutex

	systemToolSchemaCache map[string]hCommon.SystemToolSchemaDocument // 系统工具schema缓存文档

	// skillCache 已解析的 SKILL.md 文档缓存:key = SKILL.md 绝对路径。
	skillCache map[string]hCommon.SkillDocument

	//agent 的启动文件prompt,即system prompt
	agentBootstrapDocuments map[string]*agentSystemDocument
}

var agentDocRepository = pCommon.GetSingleton(func() *AgentDocumentRepository {
	return &AgentDocumentRepository{
		agentBootstrapDocuments: make(map[string]*agentSystemDocument),
		skillCache:              make(map[string]hCommon.SkillDocument),
		systemToolSchemaCache:   make(map[string]hCommon.SystemToolSchemaDocument),
	}
}).Instance()

// GetAgentDocRepository 返回全局唯一的 prompt 仓库实例
func GetAgentDocRepository() *AgentDocumentRepository {
	return agentDocRepository
}

// LoadAgentBootstrap 加载并缓存一个工作区的启动文档。只是加载agent的工作区
// 当工作区未变化(指纹相同)时直接复用缓存,避免重复读盘
// 单个文档缺失/失败不阻断整个工作区的加载
func (repo *AgentDocumentRepository) LoadAgentBootstrap(agent dataobject.TAgent) (string, error) {
	agentBootstrapRoot, ok := hCommon.BuildAgentBootstrapRoot(agent)
	if !ok {
		return "", exception.ParameterEmptyErrorEx("prompt repo: workspace root is unavailable")
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()

	var fileNames []string
	for _, item := range hCommon.StartupDocFiles {
		fileNames = append(fileNames, item.Name)
	}

	bootstrapDocument := repo.agentBootstrapDocuments[agent.AgentId]
	if bootstrapDocument != nil {
		// 判断是否修改过 *.md 文件
		if bootstrapDocument.agentBootstrapRoot == agentBootstrapRoot &&
			bootstrapDocument.loadedFingerprint == pCommon.ComputeFilesFingerprint(agentBootstrapRoot, fileNames) {
			return bootstrapDocument.loadedFingerprint, nil // 未变化,直接复用缓存
		}

		// *.md文件已经被修改过
		bootstrapDocument.documents.Clear()
		bootstrapDocument.agentBootstrapRoot = agentBootstrapRoot
	} else {
		//初次加载
		bootstrapDocument = &agentSystemDocument{
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
			bootstrapDocument.documents.Append(*doc)
		}
	}

	bootstrapDocument.loadedFingerprint = pCommon.ComputeFilesFingerprint(agentBootstrapRoot, fileNames)
	repo.agentBootstrapDocuments[agent.AgentId] = bootstrapDocument
	return bootstrapDocument.loadedFingerprint, nil
}

// LoadSystemTools 将系统工具的schema 加载进来,从数据库中的配置加载而来
func (repo *AgentDocumentRepository) LoadSystemTools(tools []dataobject.TTool) observatory.SkillLoadReport {
	observer := observatory.NewSkillObserver(len(tools))
	if len(tools) == 0 {
		return observer
	}

	//系统工具的基础目录
	systemToolRoot := filepath.Join(pCommon.GetExecutableDir(), hCommon.AgentBootstrapDir, hCommon.SystemToolDirRoot)

	// ---1) 归一化检查
	sysTools := make(map[string]dataobject.TTool, len(tools))
	seenTools := make(map[string]struct{}, len(tools))

	for _, tmpTool := range tools {
		toolName := strings.ToLower(strings.TrimSpace(tmpTool.ToolName))
		if len(toolName) == 0 {
			observer.Skip(tmpTool.ToolName, "empty tool name")
			continue
		}

		// 目录越界检查
		schemaFile, err := pCommon.SafeJoinWithRoot(systemToolRoot, filepath.Join(toolName, tmpTool.SchemaFile))
		if err != nil {
			observer.Skip(tmpTool.ToolName, "invalid tool schema file")
			continue
		}

		//重复检查
		if _, dup := seenTools[toolName]; dup {
			observer.Duplicated++
			continue
		}

		tmpTool.ToolName = toolName //回填归一化后的toolName
		seenTools[toolName] = struct{}{}
		sysTools[schemaFile] = tmpTool //此时key是input schema的路径名,绝对路径
	}

	// ---2.从缓存中取出内容
	cached := make(map[string]hCommon.SystemToolSchemaDocument, len(sysTools))
	repo.mu.Lock()
	for _, tool := range sysTools {
		if doc, hit := repo.systemToolSchemaCache[tool.ToolName]; hit {
			cached[tool.ToolName] = doc
		}
	}
	repo.mu.Unlock()

	// -- 3. 命中取缓存;未命中读盘
	out := make(map[string]hCommon.SystemToolSchemaDocument, len(sysTools))
	fresh := make(map[string]hCommon.SystemToolSchemaDocument, len(sysTools))
	for key, tool := range sysTools {
		fingerprint := pCommon.ComputeFileFingerprint(key) //此时key就是schema的路径
		if len(fingerprint) == 0 {
			observer.Skip(tool.ToolName, "file not found")
			continue
		}

		//指纹是否命中
		if doc, hit := cached[tool.ToolName]; hit && fingerprint == doc.SchemaFingerprint {
			out[key] = doc
			observer.Cached++
			continue
		}

		doc, err := ReadSystemToolSchemaDocument(key, fingerprint)
		if err != nil {
			observer.Skip(tool.ToolName, err.Error())
			continue
		}

		scriptFile, err := pCommon.SafeJoinWithRoot(systemToolRoot, filepath.Join(tool.ToolName, tool.Script))
		if err != nil {
			observer.Skip(tool.ToolName, err.Error())
			continue
		}

		doc.Visible = tool.Visible //设置对LLM是否可见的属性
		doc.Script = scriptFile

		fresh[key] = *doc
		out[key] = *doc
		observer.Reloaded++
	}

	// ---4 回写新的条目
	if len(fresh) > 0 {
		repo.mu.Lock()
		for key := range fresh {
			toolName := strings.ToLower(strings.TrimSpace(fresh[key].ToolName))
			repo.systemToolSchemaCache[toolName] = fresh[key]
		}
		repo.mu.Unlock()
	}

	observer.Loaded = len(out)
	return observer
}

// LoadSkills 解析并缓存一组 SKILL.md,返回本轮可用的技能文档(按入参顺序去重)与加载统计。
//
// 这是 SKILL.md 的**唯一读盘 + 唯一 YAML 解析入口**:工具层只消费返回文档的 FrontMatter,
// prompt 层只消费目录与正文 —— 都不再自己读文件。
func (repo *AgentDocumentRepository) LoadSkills(skillFiles []string) ([]hCommon.SkillDocument, observatory.SkillLoadReport) {
	report := observatory.NewSkillObserver(len(skillFiles))
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
		filePath, err := ResolveSkillDocPath(skillFile)
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

		doc, err := ReadSkillDocument(filePath, fingerprint)
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

// GetAgentDocuments 返回全部启动文档的副本
func (repo *AgentDocumentRepository) GetAgentDocuments(agentId string) []hCommon.AgentDocument {
	agentId = strings.TrimSpace(agentId)
	if len(agentId) == 0 {
		return nil
	}

	// 读锁:与 LoadAgentBootstrap 的“清空 + 逐条重填”互斥,否则可能读到半空的文档列表
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	prompt := repo.agentBootstrapDocuments[agentId]
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

func (repo *AgentDocumentRepository) GetSystemToolSchemaDocument(allowedList, disallowedList []string) ([]hCommon.SystemToolSchemaDocument, error) {
	var visibleList []string

	repo.mu.RLock()
	for key := range repo.systemToolSchemaCache {
		if repo.systemToolSchemaCache[key].Visible {
			//将默认读的系统工具加载进来
			visibleList = append(visibleList, key)
		}
	}
	repo.mu.RUnlock()

	if len(disallowedList) > 0 {
		denied := pCommon.NormalizeStringsSet(disallowedList)
		if len(denied) > 0 {
			kept := make([]string, 0, len(visibleList))
			for _, name := range visibleList {
				if _, no := denied[name]; no {
					continue
				}
				kept = append(kept, name)
			}
			visibleList = kept
		}
	}

	var documents []hCommon.SystemToolSchemaDocument
	repo.mu.RLock()
	defer repo.mu.RUnlock()
	for _, item := range visibleList {
		documents = append(documents, repo.systemToolSchemaCache[item])
	}

	return documents, nil
}

// Destroy 释放仓库持有的全部缓存
func (repo *AgentDocumentRepository) Destroy() {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	for _, item := range repo.agentBootstrapDocuments {
		if item == nil {
			continue
		}

		item.agentBootstrapRoot = ""
		item.loadedFingerprint = ""
		if item.documents != nil {
			item.documents.Clear()
		}
	}

	repo.skillCache = nil
	repo.systemToolSchemaCache = nil
	repo.agentBootstrapDocuments = nil
}

// ======================================= private methods =====================================

// readAgentDocumentLocked 读取单个启动文档并解析 front matter。
//   - ok=true 且 doc.Missing=false:文档存在
//   - ok=true 且 doc.Missing=true:必需文件缺失,content 为空,由组装层注入 [MISSING] 标记
//   - ok=false:可选文件缺失,直接省略
func (repo *AgentDocumentRepository) readAgentDocumentLocked(kind hCommon.AgentDocumentKind, workspaceRoot, fileName string, required bool) (*hCommon.AgentDocument, bool, error) {
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

	frontMatter, content := SplitFrontMatter(data)

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
func (repo *AgentDocumentRepository) cacheSkillLocked(doc hCommon.SkillDocument) {
	if len(repo.skillCache) >= maxSkillDocCacheEntries {
		repo.skillCache = make(map[string]hCommon.SkillDocument) // 满则整体重置,防陈旧条目无限累积
	}
	repo.skillCache[doc.SkillFile] = doc
}
