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
	"sort"
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

// AgentDocumentCache 负责从文件系统加载并缓存 prompt 相关的文档
//
// 并发约定:mu 只保护 map/列表的读改;磁盘 IO(路径解析/stat/读文件/YAML 解析)**一律在锁外**,
// 只把“取快照”与“回写新条目”两段极短操作放进锁内 —— 否则一个慢盘或大文件会阻塞所有 agent。
type AgentDocumentCache struct {
	mu sync.RWMutex

	systemToolSchemaCache map[string]hCommon.SystemToolSchemaDocument // 系统工具schema缓存文档

	// skillCache 已解析的 SKILL.md 文档缓存:key = SKILL.md 绝对路径。
	skillCache map[string]hCommon.SkillDocument

	//agent 的启动文件prompt,即system prompt
	agentBootstrapDocuments map[string]*agentSystemDocument
}

var agentDocumentCache = pCommon.GetSingleton(func() *AgentDocumentCache {
	return &AgentDocumentCache{
		agentBootstrapDocuments: make(map[string]*agentSystemDocument),
		skillCache:              make(map[string]hCommon.SkillDocument),
		systemToolSchemaCache:   make(map[string]hCommon.SystemToolSchemaDocument),
	}
}).Instance()

// GetAgentDocCache 返回全局唯一的 prompt 仓库实例
func GetAgentDocCache() *AgentDocumentCache {
	return agentDocumentCache
}

// LoadAgentBootstrap 加载并缓存一个工作区的启动文档。只是加载agent的工作区
// 当工作区未变化(指纹相同)时直接复用缓存,避免重复读盘
// 单个文档缺失/失败不阻断整个工作区的加载
func (dc *AgentDocumentCache) LoadAgentBootstrap(agent dataobject.TAgent) (string, error) {
	agentBootstrapRoot, ok := hCommon.BuildAgentBootstrapRoot(agent)
	if !ok {
		return "", exception.ParameterEmptyErrorEx("prompt repo: workspace root is unavailable")
	}

	// 1. 整组原始快照:指纹与正文同出一批 stat,二者不会自相矛盾
	snapshot, err := GetAgentFileRepository().Snapshot(agentBootstrapRoot)
	if err != nil {
		return "", err
	}

	// 2.指纹未变化 -> 复用派生结果(此时只查map内,不碰磁盘)
	dc.mu.RLock()
	cached := dc.agentBootstrapDocuments[agent.AgentId]
	reusable := snapshot.Fingerprint != "" && cached != nil &&
		cached.loadedFingerprint == snapshot.Fingerprint && cached.agentBootstrapRoot == agentBootstrapRoot
	if reusable {
		dc.mu.RUnlock()
		return snapshot.Fingerprint, nil
	}
	dc.mu.RUnlock()

	// 3.锁外派生:剥离front matter + 解析yaml
	documents := buildBootstrapDocuments(snapshot.Files)

	// 4.整体换一个新的SafeList;再挂上:读者不会看到"清空后重填"的半空状态
	loadedDocuments := pCommon.NewSafeList[hCommon.AgentDocument]()
	for i := range documents {
		loadedDocuments.Append(documents[i])
	}

	dc.mu.Lock()
	defer dc.mu.Unlock()

	dc.agentBootstrapDocuments[agent.AgentId] = &agentSystemDocument{
		agentId:            agent.AgentId,
		agentBootstrapRoot: agentBootstrapRoot,
		documents:          loadedDocuments,
		loadedFingerprint:  snapshot.Fingerprint,
	}

	return snapshot.Fingerprint, nil
}

// LoadSystemTools 将系统工具的schema 加载进来,从数据库中的配置加载而来
func (dc *AgentDocumentCache) LoadSystemTools(tools []dataobject.TTool) observatory.SkillLoadReport {
	observer := observatory.NewSkillObserver(len(tools))
	if len(tools) == 0 {
		return observer
	}

	//系统工具的基础目录
	systemToolRoot := filepath.Join(pCommon.GetExecutableDir(), hCommon.AgentBootstrapDir, hCommon.SystemToolDirRoot)

	// ---1) 归一化检查
	type toolFile struct {
		name       string //归一化工具名
		visible    bool   //来自DB配置
		schemaFile string // schema JSON 绝对路径
		scriptFile string //脚本绝对路径(仅解析路径,不读内容)
	}
	systemFiles := make([]toolFile, 0, len(tools))
	seenTools := make(map[string]struct{}, len(tools))

	for _, tmpTool := range tools {
		toolName := strings.ToLower(strings.TrimSpace(tmpTool.ToolName))
		if len(toolName) == 0 {
			observer.Skip(toolName, "empty tool name")
			continue
		}

		//重复检查
		if _, dup := seenTools[toolName]; dup {
			observer.Duplicated++
			continue
		}

		seenTools[toolName] = struct{}{}

		schemaFile, err := pCommon.SafeJoinWithRoot(systemToolRoot, filepath.Join(toolName, tmpTool.SchemaFile))
		if err != nil {
			observer.Skip(toolName, "invalid tool schema file")
			continue
		}

		scriptFile, err := pCommon.SafeJoinWithRoot(systemToolRoot, filepath.Join(toolName, tmpTool.Script))
		if err != nil {
			observer.Skip(toolName, "invalid tool script file")
			continue
		}

		systemFiles = append(systemFiles, toolFile{
			name:       toolName,
			visible:    tmpTool.Visible,
			schemaFile: schemaFile,
			scriptFile: scriptFile,
		})
	}

	// ---2.从缓存中取出内容
	cached := make(map[string]hCommon.SystemToolSchemaDocument, len(systemFiles))
	dc.mu.Lock()
	for _, f := range systemFiles {
		if doc, hit := dc.systemToolSchemaCache[f.name]; hit {
			cached[f.name] = doc
		}
	}
	dc.mu.Unlock()

	// -- 3. 命中取缓存;未命中读盘+解析(锁外,最慢的一段)
	type parsedTool struct {
		key string // 归一化的 DB 工具名:回写键必须与查找键 f.name同源
		doc hCommon.SystemToolSchemaDocument
	}
	updates := make([]parsedTool, 0, len(systemFiles))
	for _, f := range systemFiles {
		fingerprint := pCommon.ComputeFileFingerprint(f.schemaFile)
		if doc, hit := cached[f.name]; hit && len(fingerprint) > 0 && doc.SchemaFingerprint == fingerprint {
			//文件未变:复用已经解析出来的内容,visible必须来自数据库最新的配置
			doc.Visible = f.visible
			updates = append(updates, parsedTool{key: f.name, doc: doc})
			observer.Cached++
			continue
		}

		// 未命中缓存,读盘
		snapshot := ReadDocFile(f.schemaFile, MaxSkillDocSize)
		if snapshot.State != FileReady {
			observer.Skip(f.name, snapshot.SkipReason())
			continue
		}

		doc, err := ParseSystemToolSchema(snapshot, f.scriptFile, f.visible)
		if err != nil {
			observer.Skip(f.name, err.Error())
			continue
		}

		updates = append(updates, parsedTool{key: f.name, doc: *doc})
		observer.Reloaded++
	}

	// ---4 一次回写(极短)
	if len(updates) > 0 {
		dc.mu.Lock()
		for i := range updates {
			dc.systemToolSchemaCache[updates[i].key] = updates[i].doc
		}
		dc.mu.Unlock()
	}

	observer.Loaded = len(updates)
	return observer
}

// LoadSkills 解析并缓存一组 SKILL.md,返回本轮可用的技能文档(按入参顺序去重)与加载统计。
//
// 这是 SKILL.md 的**唯一读盘 + 唯一 YAML 解析入口**:工具层只消费返回文档的 FrontMatter,
// prompt 层只消费目录与正文 —— 都不再自己读文件。
func (dc *AgentDocumentCache) LoadSkills(skillFiles []string) ([]hCommon.SkillDocument, observatory.SkillLoadReport) {
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
	dc.mu.RLock()
	for _, filePath := range paths {
		if doc, hit := dc.skillCache[filePath]; hit {
			cached[filePath] = doc
		}
	}
	dc.mu.RUnlock()

	// ---- 3) 命中复用 / 未命中读盘 + 解析(锁外,最慢的一段) ----
	out := make([]hCommon.SkillDocument, 0, len(paths))
	fresh := make([]hCommon.SkillDocument, 0, len(paths))
	for _, filePath := range paths {
		fingerprint := pCommon.ComputeFileFingerprint(filePath)

		// 指纹命中:直接复用已解析文档(正文 + front matter),零读盘零解析
		if doc, hit := cached[filePath]; hit && len(fingerprint) > 0 && doc.Fingerprint == fingerprint {
			out = append(out, doc)
			report.Cached++
			continue
		}

		// 未命中
		snapshot := ReadDocFile(filePath, MaxSkillDocSize)
		if snapshot.State != FileReady {
			report.Skip(filePath, snapshot.SkipReason()) // 超大/不可读:整份跳过(不截断)
			continue
		}

		doc := ParseSkillDocument(snapshot)
		fresh = append(fresh, *doc)
		out = append(out, *doc)
		report.Reloaded++
	}

	// ---- 4) 回写新条目(写锁,极短) ----
	if len(fresh) > 0 {
		dc.mu.Lock()
		for i := range fresh {
			dc.cacheSkillLocked(fresh[i])
		}
		dc.mu.Unlock()
	}

	report.Loaded = len(out)
	return out, report
}

// GetAgentDocuments 返回全部启动文档的副本
func (dc *AgentDocumentCache) GetAgentDocuments(agentId string) []hCommon.AgentDocument {
	agentId = strings.TrimSpace(agentId)
	if len(agentId) == 0 {
		return nil
	}

	// 读锁:与 LoadAgentBootstrap 的“清空 + 逐条重填”互斥,否则可能读到半空的文档列表
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	prompt := dc.agentBootstrapDocuments[agentId]
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

func (dc *AgentDocumentCache) GetSystemToolSchemaDocument(allowedList, disallowedList []string) ([]hCommon.SystemToolSchemaDocument, error) {
	var visibleList []string

	dc.mu.RLock()
	for key := range dc.systemToolSchemaCache {
		if dc.systemToolSchemaCache[key].Visible {
			//将默认读的系统工具加载进来
			visibleList = append(visibleList, key)
		}
	}
	dc.mu.RUnlock()

	// 1. allowed-tool 非空即白名单
	if allowed := pCommon.NormalizeStringsSet(allowedList); len(allowed) > 0 {
		kept := make([]string, 0, len(visibleList))
		for _, name := range visibleList {
			if _, ok := allowed[name]; ok {
				kept = append(kept, name)
			}
		}

		visibleList = kept
	}

	// 2. disallowed-tools 优先级高于 allowed-tools:显示禁止一定生效
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

	// 3. 排序
	sort.Strings(visibleList)

	var documents []hCommon.SystemToolSchemaDocument
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	for _, item := range visibleList {
		doc, ok := dc.systemToolSchemaCache[item]
		if !ok {
			continue
		}
		documents = append(documents, doc)
	}

	return documents, nil
}

// Destroy 释放仓库持有的全部缓存
func (dc *AgentDocumentCache) Destroy() {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	for _, item := range dc.agentBootstrapDocuments {
		if item == nil {
			continue
		}

		item.agentBootstrapRoot = ""
		item.loadedFingerprint = ""
		if item.documents != nil {
			item.documents.Clear()
		}
	}

	dc.skillCache = nil
	dc.systemToolSchemaCache = nil
	dc.agentBootstrapDocuments = nil
}

// ======================================= private methods =====================================

// cacheSkillLocked 写入技能解析缓存(调用方需持有 repo.mu)
func (dc *AgentDocumentCache) cacheSkillLocked(doc hCommon.SkillDocument) {
	if len(dc.skillCache) >= maxSkillDocCacheEntries {
		dc.skillCache = make(map[string]hCommon.SkillDocument) // 满则整体重置,防陈旧条目无限累积
	}
	dc.skillCache[doc.SkillFile] = doc
}

// buildBootstrapDocuments 把原始启动文件快照派生为 prompt 视图:
// 剥离 front matter(正文用于注入 system prompt)、解析 YAML 元数据(summary/title/readWhen)。
//
// 注入语义:
//   - 缺失 + 必需 -> 保留占位(Missing=true),由组装层注入 [MISSING] 提示待创建;
//   - 缺失 + 可选 -> 省略;
//   - 存在但正文不可用(超限/读失败)-> **告警并省略**,不注入占位:文件确实存在,
//     不能向模型谎报"不存在";缺内容即缺文档;
//   - 正常 -> 注入剥离 front matter 后的正文。
func buildBootstrapDocuments(items []StartupFileSnapshotItem) []hCommon.AgentDocument {
	out := make([]hCommon.AgentDocument, 0, len(items))

	for i := range items {
		item := items[i]

		doc := hCommon.AgentDocument{
			Kind:     item.Spec.Kind,
			FileName: item.Spec.Name,
			Required: item.Spec.Required,
			ByteSize: item.ByteSize,
			Missing:  item.State != FileReady,
		}

		switch item.State {
		case FileUnavailable:
			logging.LogWarnF("prompt repo: load %s skipped: %s", item.Spec.Name, item.Reason)
			continue
		case FileMissing:
			if item.Spec.Required {
				out = append(out, doc) // 必须文件:保留占位
			}

			continue // 可选文件缺失:省略
		}

		frontMatter, content := SplitFrontMatter([]byte(item.Content))
		doc.Content = content
		if len(frontMatter) > 0 {
			if err := yaml.Unmarshal([]byte(frontMatter), &doc.FrontMatter); err != nil {
				logging.LogWarnF("prompt repo: parse %s front matter error:%s", item.Spec.Name, err.Error())
			}
		}

		out = append(out, doc)
	}

	return out
}
