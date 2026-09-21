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

package repo

import (
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v2"
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

	documents *pCommon.SafeList[hCommon.AgentDocument]

	// 加载时记录的文件指纹,用于判断文件是否发生变化
	loadedFingerprint string
}

// maxSkillDocCacheEntries 技能文档缓存条目上限,超过则整体清空(防陈旧条目无限累积)
const maxSkillDocCacheEntries = 256

// SkillLoadReport 一次 LoadSkills 的观测结果(命中/重载/跳过 + 原因)
type SkillLoadReport struct {
	Requested  int // 入参技能数
	Loaded     int // 返回的可用技能数
	Cached     int // 命中缓存(零读盘零解析)
	Reloaded   int // 首次加载或指纹变化:读盘 + YAML 解析
	Duplicated int // 重复绑定被去掉的条数

	// Skipped 被跳过的技能:分两批记录 —— 先记入参非法(目录为空/越界),
	// 再记读盘阶段失败(文件缺失/过大/YAML 处理失败)。每条带 SkillFile 可唯一定位。
	Skipped []SkillSkipReason
}

// SkillSkipReason 一个被跳过的技能及其原因
type SkillSkipReason struct {
	SkillFile string
	Reason    string
}

// skip 记录一次跳过
func (r *SkillLoadReport) skip(skillFile, reason string) {
	r.Skipped = append(r.Skipped, SkillSkipReason{SkillFile: skillFile, Reason: reason})
}

// LogSummary 按调用方给定的 scope(如 "chat service: agent=...,turn=...")打一行加载结果。
//   - 无跳过:debug 一行 —— 命中/重载计数就是“每轮零读盘”的线上证据;
//   - 有跳过:warn 并列出原因 —— 否则技能文件坏了只会表现为“模型忽然不再调该工具”。
func (r *SkillLoadReport) LogSummary(scope string) {
	if len(r.Skipped) == 0 {
		logging.LogDebugF("%s skills: requested=%d loaded=%d cached=%d reloaded=%d duplicated=%d",
			scope, r.Requested, r.Loaded, r.Cached, r.Reloaded, r.Duplicated)
		return
	}

	logging.LogWarnF("%s skills: requested=%d loaded=%d cached=%d reloaded=%d duplicated=%d skipped=%v",
		scope, r.Requested, r.Loaded, r.Cached, r.Reloaded, r.Duplicated, r.Skipped)
}

// PromptRepository 负责从文件系统加载并缓存 prompt 相关的文档
//
// 并发约定:mu 只保护 map/列表的读改;磁盘 IO(路径解析/stat/读文件/YAML 解析)**一律在锁外**,
// 只把“取快照”与“回写新条目”两段极短操作放进锁内 —— 否则一个慢盘或大文件会阻塞所有 agent。
type PromptRepository struct {
	mu sync.RWMutex

	promptMap map[string]*agentPrompt

	// skillCache 已解析的 SKILL.md 文档缓存:key = SKILL.md 绝对路径。
	skillCache map[string]hCommon.SkillDocument
}

// IPromptRepository 定义 prompt 文档仓库的对外能力
type IPromptRepository interface {
	LoadAgentBootstrap(agent dataobject.TAgent) error

	// LoadSkills 是 SKILL.md 的唯一读盘 + YAML 解析入口(按文件指纹缓存)。
	LoadSkills(skillFiles []string) ([]hCommon.SkillDocument, SkillLoadReport)

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

// LoadSkills 解析并缓存一组 SKILL.md,返回本轮可用的技能文档(按入参顺序去重)与加载统计。
//
// 这是 SKILL.md 的**唯一读盘 + 唯一 YAML 解析入口**:工具层只消费返回文档的 FrontMatter,
// prompt 层只消费目录与正文 —— 都不再自己读文件。
func (repo *PromptRepository) LoadSkills(skillFiles []string) ([]hCommon.SkillDocument, SkillLoadReport) {
	report := SkillLoadReport{Requested: len(skillFiles)}
	if len(skillFiles) == 0 {
		return nil, report
	}

	// ---- 1) 归一化(锁外):技能目录配置 → SKILL.md 绝对路径,并去重 ----
	paths := make([]string, 0, len(skillFiles))
	seen := make(map[string]struct{}, len(skillFiles))
	for _, skillFile := range skillFiles {
		skillFile = strings.TrimSpace(skillFile)
		if len(skillFile) == 0 {
			report.skip(skillFile, "empty skill dir")
			continue
		}

		// 越界校验(相对 skills 根解析 + 真实路径包含性)在 common 里单点收敛
		filePath, err := hCommon.ResolveSkillDocPath(skillFile)
		if err != nil {
			report.skip(skillFile, err.Error())
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
			report.skip(filePath, "file not found")
			continue
		}

		// 指纹命中:直接复用已解析文档(正文 + front matter),零读盘零解析
		if doc, hit := cached[filePath]; hit && doc.Fingerprint == fingerprint {
			out = append(out, doc)
			report.Cached++
			continue
		}

		doc, err := readSkillDoc(filePath, fingerprint)
		if err != nil {
			report.skip(filePath, err.Error()) // 超大/不可读:整份跳过(不截断)
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

// readSkillDoc 读取并解析一个 SKILL.md(唯一的读盘 + 唯一的 YAML 解析点)。
// YAML 失败只告警不丢弃文档:正文是 SOP 注入的唯一来源,一个 front matter 笔误不该让
// 技能连正文都进不了 prompt(元数据为空时目录/关键词自然也就为空)。
func readSkillDoc(filePath, fingerprint string) (*hCommon.SkillDocument, error) {
	data, size, err := pCommon.ReadFile(filePath, hCommon.MaxSkillDocSize)
	if err != nil {
		return nil, err
	}

	frontMatter, content := hCommon.SplitFrontMatter(data)

	doc := &hCommon.SkillDocument{
		SkillId:     filepath.Base(filepath.Dir(filePath)), // 技能目录名(如 weather / npa-report)
		SkillFile:   filePath,
		Fingerprint: fingerprint,
		Content:     content,
		ByteSize:    size,
	}
	if len(frontMatter) > 0 {
		if err := yaml.Unmarshal([]byte(frontMatter), &doc.FrontMatter); err != nil {
			logging.LogWarnF("prompt repo: parse skill %s front matter error:%s", filePath, err.Error())
		}
	}

	return doc, nil
}

// cacheSkillLocked 写入技能解析缓存(调用方需持有 repo.mu)
func (repo *PromptRepository) cacheSkillLocked(doc hCommon.SkillDocument) {
	if len(repo.skillCache) >= maxSkillDocCacheEntries {
		repo.skillCache = make(map[string]hCommon.SkillDocument) // 满则整体重置,防陈旧条目无限累积
	}
	repo.skillCache[doc.SkillFile] = doc
}
