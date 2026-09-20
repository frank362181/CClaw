/**
* Agent 启动文件(Startup File),以及SKILL文件管理仓库
*
* 职责:面向"查看/编辑 agent 启动文件"的管理接口,读取工作区内的启动文件
*   (AGENTS.md/SOUL.md/...):
*   - GetStartupFiles:  启动文件清单(仅元数据,不含正文)
*   - GetStartupFile:   单个启动文件的原始内容
*
* 与 PromptRepository 解耦:PromptRepository 只负责 prompt 组装缓存,
* 本仓库独立负责启动文件的读取,并持有自己的轻量缓存:
*   - 列表:TTL 缓存,TTL 内零磁盘访问
*   - 单文件正文:按 (size, mtime) 指纹缓存,文件未变则零读盘;
*     打开时 O_NOFOLLOW 防 TOCTOU 符号链接替换(Unix)
*
* 安全路径规则与 prompt 组装共用 utility.ResolveStartupPath。
**/

package repo

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	hCommon "my.favorite/cclawservice/harness/common"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/exception"
)

// startupFileContent 单个启动文件的指纹缓存条目
// 以 (size, mtime) 作为文件指纹:文件未变化时直接复用,零读盘。
type startupFileContent struct {
	size    int64
	modTime int64 // 文件修改时间(纳秒)
	content string
}

// startupFileEntry 一个工作区的启动文件缓存
//   - list:    启动文件清单(不含正文),按 TTL 失效
//   - contents:单文件正文,按指纹命中
type startupFileEntry struct {
	loadedAt int64 // 列表加载时间(秒级时间戳)
	list     []hCommon.AgentDocument
	contents map[string]startupFileContent // key: 文件名
}

// startupFileCache 启动文件读取的轻量缓存(按工作区划分)
type startupFileCache struct {
	mu      sync.Mutex
	entries map[string]*startupFileEntry // key = agent的bootstrap dir
}

// maxStartupFileCacheEntries 缓存条目上限,超过则整体清空(防陈旧条目无限累积)
const maxStartupFileCacheEntries = 64

// AgentFileRepository 负责读取 agent 工作区内的启动文件(清单 + 内容),带轻量缓存。
type AgentFileRepository struct {
	startupCache *startupFileCache
}

// IAgentFileRepository 定义启动文件仓库的对外能力
type IAgentFileRepository interface {
	GetStartupFiles(bootstrapRoot string) []hCommon.AgentDocument
	GetStartupFile(bootstrapRoot, fileName string) (*hCommon.AgentDocument, error)

	Destroy()
}

var agentFileRepository = pCommon.GetSingleton(func() *AgentFileRepository {
	return &AgentFileRepository{
		startupCache: &startupFileCache{
			entries: make(map[string]*startupFileEntry),
		},
	}
}).Instance()

// GetAgentFileRepository 返回全局唯一的启动文件仓库实例
func GetAgentFileRepository() IAgentFileRepository {
	return agentFileRepository
}

// GetStartupFiles 返回某工作区全部启动文件的清单(仅元数据,不读正文)。
// 按 StartupDocFiles 固定顺序返回;不存在的文件以 missing=true 标记;
// 列表项复用 AgentDocument,不携带正文(Content/FrontMatter 为空)。
// 性能:结果按工作区做 TTL 轻量缓存,TTL 内零磁盘访问。
// 安全:只统计白名单内的启动文件;符号链接视为缺失(与正文读取的 O_NOFOLLOW 语义一致)。
func (repo *AgentFileRepository) GetStartupFiles(agentBootstrapRoot string) []hCommon.AgentDocument {
	agentBootstrapRoot = strings.TrimSpace(agentBootstrapRoot)
	if len(agentBootstrapRoot) == 0 {
		return nil
	}

	cache := repo.startupCache
	cache.mu.Lock()
	defer cache.mu.Unlock()

	entry := cache.entry(agentBootstrapRoot)
	now := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	if entry.list != nil && now-entry.loadedAt < hCommon.StartupFileCacheTTLSeconds {
		return entry.list
	}

	entry.list = repo.buildStartupFilesList(agentBootstrapRoot)
	entry.loadedAt = now
	return entry.list
}

// GetStartupFile 返回某工作区中指定启动文件的原始内容(含 front matter)。
// 安全:文件名必须命中 StartupDocFiles 白名单,并经由 ResolveStartupPath
// 校验防目录穿越与符号链接逃逸;打开时 O_NOFOLLOW 防 TOCTOU 符号链接替换;
// 内容受 MaxAgentDocSize 大小上限约束(防内存耗尽)。
// 性能:单文件正文按 (size, mtime) 指纹缓存,文件未变则零读盘。
// 文件缺失时返回 Missing=true 与空内容(不视为错误),便于前端展示"待创建"。
func (repo *AgentFileRepository) GetStartupFile(agentBootstrapRoot, fileName string) (*hCommon.AgentDocument, error) {
	agentBootstrapRoot = strings.TrimSpace(agentBootstrapRoot)
	fileName = strings.TrimSpace(fileName)
	if len(agentBootstrapRoot) == 0 || len(fileName) == 0 {
		return nil, exception.ParameterEmptyErrorEx("startup file repo: workspace root or file name is empty")
	}

	// 白名单校验:仅允许 StartupDocFiles 清单中的启动文件
	item, allowed := hCommon.FindStartupDocFile(fileName)
	if !allowed {
		return nil, exception.FilePathErrorEx("startup file repo: not a startup file: " + fileName)
	}

	filePath, err := hCommon.ResolveStartupPath(agentBootstrapRoot, fileName)
	if err != nil {
		return nil, err
	}

	doc := &hCommon.AgentDocument{
		Kind:     item.Kind,
		FileName: item.Name,
		Required: item.Required,
		Missing:  true,
	}

	// O_NOFOLLOW 打开(Unix):符号链接/目录/缺失统一按缺失返回,不泄漏具体原因
	f, stat, err := pCommon.OpenFileNoFollow(filePath)
	if err != nil {
		return doc, nil
	}
	defer func() { _ = f.Close() }()

	if stat.IsDir() {
		return doc, nil // 目录按缺失
	}
	if stat.Size() > hCommon.MaxAgentDocSize {
		return nil, errors.New("startup file too large: " + fileName)
	}

	modTime := stat.ModTime().UnixNano()

	cache := repo.startupCache
	cache.mu.Lock()
	defer cache.mu.Unlock()

	entry := cache.entry(agentBootstrapRoot)
	if c, hit := entry.contents[fileName]; hit && c.size == stat.Size() && c.modTime == modTime {
		doc.Content = c.content
		doc.ByteSize = c.size
		doc.Missing = false
		return doc, nil
	}

	// 从已打开句柄按上限读取(即使文件在 fstat 后增长也不会超限)
	data, readErr := io.ReadAll(io.LimitReader(f, hCommon.MaxAgentDocSize+1))
	if readErr != nil {
		return nil, readErr
	}
	if int64(len(data)) > hCommon.MaxAgentDocSize {
		return nil, errors.New("startup file too large: " + fileName)
	}

	doc.Content = string(data) // 原始正文(含 front matter),供前端展示/编辑
	doc.ByteSize = int64(len(data))
	doc.Missing = false
	entry.contents[fileName] = startupFileContent{size: int64(len(data)), modTime: modTime, content: doc.Content}
	return doc, nil
}

// Destroy 释放启动文件读取缓存
func (repo *AgentFileRepository) Destroy() {
	repo.startupCache.mu.Lock()
	defer repo.startupCache.mu.Unlock()
	repo.startupCache.entries = make(map[string]*startupFileEntry)
}

// ======================= private functions & methods ================================
// buildStartupFilesList 按白名单统计工作区启动文件的元数据(不读正文)。
// 性能:一次性解析工作区根的真实路径,供全部文件复用(避免重复 EvalSymlinks)。
func (repo *AgentFileRepository) buildStartupFilesList(agentBootstrapRoot string) []hCommon.AgentDocument {
	rootAbs, err := filepath.Abs(agentBootstrapRoot)
	if err != nil {
		return nil
	}
	realRoot := pCommon.ResolveRealPath(filepath.Clean(rootAbs))

	list := make([]hCommon.AgentDocument, 0, len(hCommon.StartupDocFiles))
	for _, item := range hCommon.StartupDocFiles {
		doc := hCommon.AgentDocument{
			Kind:     item.Kind,
			FileName: item.Name,
			Required: item.Required,
			Missing:  true,
		}

		filePath, joinErr := pCommon.SafeJoinWithRoot(realRoot, filepath.Join(agentBootstrapRoot, item.Name))
		if joinErr == nil {
			// 符号链接视为缺失,与正文读取的 O_NOFOLLOW 语义一致
			if stat, statErr := os.Lstat(filePath); statErr == nil && !stat.IsDir() && stat.Mode()&os.ModeSymlink == 0 {
				doc.ByteSize = stat.Size()
				doc.Missing = false
			}
		}
		list = append(list, doc)
	}

	return list
}

// entry 取/建指定工作区的缓存条目(调用方需持有 mu)
func (c *startupFileCache) entry(bootstrapRoot string) *startupFileEntry {
	entry := c.entries[bootstrapRoot]
	if entry != nil {
		return entry
	}
	if len(c.entries) >= maxStartupFileCacheEntries {
		c.entries = make(map[string]*startupFileEntry)
	}
	entry = &startupFileEntry{contents: make(map[string]startupFileContent)}
	c.entries[bootstrapRoot] = entry
	return entry
}
