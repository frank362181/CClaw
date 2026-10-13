/**
* Agent 启动文件(Startup File),以及SKILL文件管理仓库
*
* 职责:面向"查看/编辑 agent 启动文件"的管理接口,读取工作区内的启动文件
*   (AGENTS.md/SOUL.md/...):
*   - GetStartupFiles:  启动文件清单(仅元数据,不含正文)
*   - GetStartupFile:   单个启动文件的原始内容
*
* 本类是本系统中所有的agent,skill等document的唯一的数据源
**/

package ctxmgr

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	hCommon "my.favorite/cclawservice/harness/common"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/exception"
)

// startupFileContent 单个启动文件的指纹缓存条目,如AGENTS.md,SOUL.md等
// 以 (size, mtime) 作为文件指纹:文件未变化时直接复用,零读盘。
type startupFileContent struct {
	exists  bool // 表示内容是否存在
	size    int64
	modTime int64 // 文件修改时间(纳秒)

	hasContent bool   // content是否已经实际读盘(清单路径只做stat,不读取正文)
	content    string //原始正文(包含front matter)
	reason     string //存在但正文不可用时的原因(仅诊断,不参与指纹)
}

// 表示一个Agent的启动文件缓存
type startupFileBucket struct {
	resolvedRoot string // 已经解析真实路径的工作区根(避免每次重复调用EvalSymlinks)
	loadedAt     int64  // 最近一次清单stat的时间(秒级时间戳)
	fingerprint  string //整组指纹

	startupFiles map[string]startupFileContent // 一个agent下面的7个启动文件
}

// startupFileCache 启动文件读取的轻量缓存(按工作区划分)
type startupFileCache struct {
	mu      sync.Mutex
	buckets map[string]*startupFileBucket // key = agent的bootstrap root
}

// maxStartupFileCacheEntries 缓存条目上限,超过则整体清空(防陈旧条目无限累积)
const maxStartupFileCacheEntries = 64

// AgentFileRepository 负责读取 agent 工作区内的启动文件(清单 + 内容),带轻量缓存。
type AgentFileRepository struct {
	startupCache *startupFileCache
}

var agentFileRepository = pCommon.GetSingleton(func() *AgentFileRepository {
	return &AgentFileRepository{
		startupCache: &startupFileCache{
			buckets: make(map[string]*startupFileBucket),
		},
	}
}).Instance()

// GetAgentFileRepository 返回全局唯一的启动文件仓库实例
func GetAgentFileRepository() *AgentFileRepository {
	return agentFileRepository
}

// GetStartupFiles 返回某工作区全部启动文件的清单(仅元数据,不读正文)。
// 按 StartupDocFiles 固定顺序返回;不存在的文件以 missing=true 标记;
// 列表项复用 AgentDocument,不携带正文(Content/FrontMatter 为空)。
// 性能:结果按工作区做 TTL 轻量缓存,TTL 内零磁盘访问。
// 安全:只统计白名单内的启动文件;符号链接视为缺失(与正文读取的 O_NOFOLLOW 语义一致)
func (repo *AgentFileRepository) GetStartupFiles(agentBootStartupRoot string) []hCommon.AgentDocument {
	agentBootstrapRoot := strings.TrimSpace(agentBootStartupRoot)
	if len(agentBootstrapRoot) == 0 {
		return nil
	}

	cache := repo.startupCache
	now := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp

	// 短锁:是否命中缓存;否则只区"待复用信息"就放缩,磁盘IO在锁外
	cache.mu.Lock()
	bucket := cache.buckets[agentBootstrapRoot]
	if bucket != nil && bucket.loadedAt > 0 && now-bucket.loadedAt <= hCommon.StartupFileCacheTTLSeconds {
		list := startupFilesMetadata(bucket.startupFiles)
		cache.mu.Unlock()
		return list
	}

	// 未命中缓存,previous为空
	resolvedRoot := ""
	previous := map[string]startupFileContent{}
	if bucket != nil {
		resolvedRoot, previous = bucket.resolvedRoot, bucket.clone()
	}
	cache.mu.Unlock()

	// 判断解析的到startupFiles所在的路径
	if resolvedRoot == "" {
		resolvedRoot = pCommon.ResolveRealPath(agentBootstrapRoot)
		if len(resolvedRoot) == 0 {
			return nil
		}
	}

	// 锁外:stat 全部白名单文件(不读取正文）
	startupFiles := readStartupFiles(resolvedRoot, hCommon.StartupDocFiles, previous, false)

	cache.mu.Lock()
	defer cache.mu.Unlock()

	bucket = cache.bucket(agentBootstrapRoot)
	bucket.resolvedRoot = resolvedRoot
	bucket.startupFiles = startupFiles
	bucket.fingerprint = computeStartupFileFingerprint(startupFiles)
	bucket.loadedAt = now

	return startupFilesMetadata(startupFiles)
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

	repo.startupCache.mu.Lock()
	resolvedRoot, previous := repo.getPrevious(agentBootstrapRoot)
	repo.startupCache.mu.Unlock()
	if len(resolvedRoot) == 0 {
		resolvedRoot = pCommon.ResolveRealPath(agentBootstrapRoot)
		if len(resolvedRoot) == 0 {
			return nil, exception.FilePathErrorEx("startup file repo: workspace root is empty")
		}
	}

	filePath, err := hCommon.ResolveStartupPath(resolvedRoot, item.Name)
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
	entry := startupFileContent{exists: true, size: stat.Size(), modTime: modTime}

	// 指纹命中,复用
	if prev, hit := previous[item.Name]; hit && prev.hasContent && prev.size == stat.Size() && prev.modTime == modTime {
		entry.content = prev.content
		entry.hasContent = true
	} else {
		// 从已经打开的文件句柄上限读取(即使文件在fstat后增长也不会超限)
		data, err := io.ReadAll(io.LimitReader(f, hCommon.MaxAgentDocSize+1))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > hCommon.MaxAgentDocSize {
			return nil, errors.New("startup file too large: " + fileName)
		}

		entry.hasContent = true
		entry.content = string(data)
	}

	// 只更新一个文件并重新计算指纹
	repo.startupCache.mu.Lock()
	defer repo.startupCache.mu.Unlock()

	bucket := repo.startupCache.bucket(agentBootstrapRoot)
	bucket.resolvedRoot = resolvedRoot
	bucket.startupFiles[item.Name] = entry
	bucket.fingerprint = computeStartupFileFingerprint(bucket.startupFiles)

	doc.Content = entry.content // 原始正文(含 front matter),供前端展示/编辑
	doc.ByteSize = entry.size
	doc.Missing = false
	return doc, nil
}

func (repo *AgentFileRepository) Snapshot(agentBootstrapRoot string) (*StartupSnapshot, error) {
	agentBootstrapRoot = strings.TrimSpace(agentBootstrapRoot)
	if len(agentBootstrapRoot) == 0 {
		return nil, exception.ParameterEmptyErrorEx("startup file repo: workspace root is empty")
	}

	repo.startupCache.mu.Lock()
	resolvedRoot, previous := repo.getPrevious(agentBootstrapRoot)
	repo.startupCache.mu.Unlock()

	if len(resolvedRoot) == 0 {
		resolvedRoot = pCommon.ResolveRealPath(agentBootstrapRoot)
		if len(resolvedRoot) == 0 {
			return nil, exception.FilePathErrorEx("startup file repo: workspace root is empty")
		}
	}

	// withContent = true,快照必须带正文,否则prompt层拿到的是空文档
	startupFiles := readStartupFiles(resolvedRoot, hCommon.StartupDocFiles, previous, true)
	fingerprint := computeStartupFileFingerprint(startupFiles)

	cache := repo.startupCache
	cache.mu.Lock()
	defer cache.mu.Unlock()

	bucket := cache.bucket(agentBootstrapRoot)
	bucket.resolvedRoot = resolvedRoot
	bucket.startupFiles = startupFiles
	bucket.fingerprint = fingerprint
	bucket.loadedAt = pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp

	files := startupFileSnapshotItems(bucket.startupFiles)
	return &StartupSnapshot{Fingerprint: fingerprint, Files: files}, nil
}

// Destroy 释放启动文件读取缓存
func (repo *AgentFileRepository) Destroy() {
	repo.startupCache.mu.Lock()
	defer repo.startupCache.mu.Unlock()

	repo.startupCache.buckets = make(map[string]*startupFileBucket)
}

// ======================= private functions & methods ================================

// 根据agent的根目录获取缓存
func (repo *AgentFileRepository) getPrevious(root string) (string, map[string]startupFileContent) {
	cache := repo.startupCache

	bucket := cache.buckets[root]
	if bucket == nil {
		return "", nil
	}

	return bucket.resolvedRoot, bucket.clone()
}

// bucket 取/建指定工作区的缓存条目(调用方需持有 mu)
func (c *startupFileCache) bucket(bootstrapRoot string) *startupFileBucket {
	b := c.buckets[bootstrapRoot]
	if b != nil {
		return b
	}
	if len(c.buckets) >= maxStartupFileCacheEntries {
		c.buckets = make(map[string]*startupFileBucket)
	}
	b = &startupFileBucket{startupFiles: make(map[string]startupFileContent)}
	c.buckets[bootstrapRoot] = b
	return b
}

func (b *startupFileBucket) clone() map[string]startupFileContent {
	if b == nil || b.startupFiles == nil {
		return nil
	}

	out := make(map[string]startupFileContent, len(b.startupFiles))
	for name, entry := range b.startupFiles {
		out[name] = entry
	}

	return out
}

// =================== functions & methods for module ============================

func computeStartupFileFingerprint(startupFiles map[string]startupFileContent) string {
	var sb strings.Builder
	for _, spec := range hCommon.StartupDocFiles {
		entry := startupFiles[spec.Name]
		if !entry.exists {
			_, _ = fmt.Fprintf(&sb, "%smissing;", spec.Name)
			continue
		}

		_, _ = fmt.Fprintf(&sb, "%s:%d:%d;", spec.Name, entry.size, entry.modTime)
	}

	return sb.String()
}

func readStartupFiles(resolvedRoot string, startDocSpec []hCommon.StartupDocSpec, previous map[string]startupFileContent, withContent bool) map[string]startupFileContent {
	startupContents := make(map[string]startupFileContent, len(startDocSpec))

	for _, spec := range startDocSpec {
		entry := startupFileContent{exists: false}

		startupFileName, err := pCommon.SafeJoinWithRoot(resolvedRoot, filepath.Join(resolvedRoot, spec.Name))
		if err != nil {
			continue
		}

		if info, statErr := os.Lstat(startupFileName); statErr == nil && !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			entry.exists = true
			entry.size = info.Size()
			entry.modTime = info.ModTime().UnixNano()

			if prev, hit := previous[spec.Name]; hit && prev.exists && prev.size == entry.size && prev.modTime == entry.modTime &&
				(!withContent || prev.hasContent) {
				entry.hasContent = prev.hasContent
				entry.content = prev.content
				entry.reason = prev.reason
			} else if withContent {
				// 读取整个启动文件;取不到时记录原因(超限/读失败),由上层决定"省略"而非"占位"
				content, ok, reason := readStartupFileContent(startupFileName)
				entry.hasContent = ok
				entry.content = content
				entry.reason = reason
			}
		}

		startupContents[spec.Name] = entry
	}

	return startupContents
}

// 获取Agent启动文件的元数据
func startupFilesMetadata(files map[string]startupFileContent) []hCommon.AgentDocument {
	list := make([]hCommon.AgentDocument, 0, len(files))
	for _, spec := range hCommon.StartupDocFiles {
		startupFile := files[spec.Name]
		list = append(list, hCommon.AgentDocument{
			Kind:     spec.Kind,
			FileName: spec.Name,
			Required: spec.Required,
			Missing:  !startupFile.exists,
			ByteSize: startupFile.size,
		})
	}

	return list
}

// startupFileSnapshotItems 把缓存条目投影为整组快照项(供 prompt 层派生)。
// 三态必须区分:缺失与"存在但正文不可用"在上层处理不同
// (前者对必需文件注入占位,后者只告警省略)。
func startupFileSnapshotItems(startupFiles map[string]startupFileContent) []StartupFileSnapshotItem {
	items := make([]StartupFileSnapshotItem, 0, len(hCommon.StartupDocFiles))

	for _, spec := range hCommon.StartupDocFiles {
		entry := startupFiles[spec.Name]
		item := StartupFileSnapshotItem{Spec: spec, ByteSize: entry.size}

		switch {
		case !entry.exists:
			item.State = StartupFileMissing
		case entry.hasContent:
			item.State = StartupFileReady
			item.Content = entry.content
		default:
			item.State = StartupFileUnavailable
			item.Reason = entry.reason
		}

		items = append(items, item)
	}

	return items
}

// readStartupFileContent 以 O_NOFOLLOW 打开并读取正文。
// 关键:大小判断与读取都发生在**同一个已打开句柄**上 —— 既防 TOCTOU 符号链接替换,
// 也防"fstat 之后文件被替换/增长"绕过 MaxAgentDocSize 上限。
// 返回 (内容, 是否成功, 失败原因);原因仅供上层记日志,不向外部暴露具体路径。
func readStartupFileContent(startupFileName string) (string, bool, string) {
	f, stat, err := pCommon.OpenFileNoFollow(startupFileName)
	if err != nil {
		// 符号链接/缺失/权限不足统一折算为"不可用",不泄漏具体原因
		return "", false, err.Error()
	}
	defer func() { _ = f.Close() }()

	if stat.IsDir() {
		return "", false, "not a regular file"
	}
	if stat.Size() > hCommon.MaxAgentDocSize {
		return "", false, fmt.Sprintf("startup file too large (limit=%d bytes)", hCommon.MaxAgentDocSize)
	}

	data, err := io.ReadAll(io.LimitReader(f, hCommon.MaxAgentDocSize+1))
	if err != nil {
		return "", false, err.Error()
	}
	if int64(len(data)) > hCommon.MaxAgentDocSize {
		return "", false, fmt.Sprintf("startup file too large (limit=%d bytes)", hCommon.MaxAgentDocSize)
	}

	return string(data), true, ""
}
