package ctxmgr

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v2"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
	pException "my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
)

// BuildSkillRoot 返回技能根目录 <exeDir>/cclaw/skills。
// 所有技能都位于该目录之下,一个子目录一个技能(目录名即 t_skill.skill_dir)。
func BuildSkillRoot() (string, bool) {
	skillRoot := filepath.Join(pCommon.GetExecutableDir(), hCommon.AgentBootstrapDir, hCommon.SkillDirRoot)
	return skillRoot, true
}

// NormalizeSkillDir 把 skill 目录配置规整为“技能目录名”(相对 skills 根的安全相对路径)。
//
// 布局约定(一个目录一个技能):技能文档固定为 <exeDir>/cclaw/skills/<skillDir>/SKILL.md。
// 兼容两种写法(历史数据存的是后者):
//   - 目录形式(推荐):"npa-report"
//   - 文档形式(历史,等价):"npa-report/SKILL.md" —— 取其上层层级
//
// 安全:必须是安全的相对路径(非空、非绝对、无 "."/".." 段与冗余分隔符),
// 保证技能一律落在 skills 根之内;非法时返回业务错误(与 ValidateAgent 口径一致)。
func NormalizeSkillDir(skillDir string) (string, error) {
	dir := strings.TrimSpace(filepath.ToSlash(skillDir))
	if dir == "" {
		return pCommon.BlankString, pException.ParameterEmptyErrorEx("skill dir is empty")
	}

	// 历史写法:配置直接指向 SKILL.md(或任何层级的该文件),取其上层层级
	if filepath.Base(dir) == SkillDocFileName {
		dir = strings.TrimSpace(filepath.ToSlash(filepath.Dir(dir)))
	}
	if !pCommon.IsSafeDir(dir) {
		return pCommon.BlankString, pException.ParameterEmptyErrorEx("skill dir is an illegal path: " + skillDir)
	}

	return dir, nil
}

// GetContextPolicy 按 Agent 与模型配置推导装配策略。
//   - context_window 来自 t_llm;
//   - reverse_token_floor 是 t_agent 上**为系统提示预留的最小额度**(保护启动文档不被历史挤掉);
//   - max_history_tokens 是 t_agent 上**历史回放上限**(压缩节奏旋钮):
//     0/未配置 → 用 DefaultMaxHistoryTokens;值为负 → 不限制。
func GetContextPolicy(agent dataobject.TAgent, contextWindow int) hCommon.ContextPolicy {
	policy := defaultContextPolicy()

	policy.ContextWindow = contextWindow
	policy.SystemPromptReserveTokens = agent.ReverseTokenFloor
	policy.MaxHistoryTokens = agent.MaxHistoryTokens

	return policy
}

func NormalizeContextPolicy(policy hCommon.ContextPolicy) hCommon.ContextPolicy {
	if policy.OutputReserve <= 0 {
		policy.OutputReserve = DefaultOutputReserveTokens
	}
	if policy.ToolReplayMaxRunes <= 0 {
		policy.ToolReplayMaxRunes = DefaultToolReplayMaxRunes
	}
	if policy.SystemPromptReserveTokens < 0 {
		policy.SystemPromptReserveTokens = 0
	}
	if policy.ToolsReserveTokens < 0 {
		policy.ToolsReserveTokens = 0
	}
	// 历史回放上限:0 = 用默认;负数 = 显式不限制。默认值统一在这里兜 ——
	// 手写的 ContextPolicy(测试、特殊调用方)不会因为漏填而变成"不限制"。
	if policy.MaxHistoryTokens == 0 {
		policy.MaxHistoryTokens = DefaultMaxHistoryTokens
	}

	return policy
}

// ResolveSkillDocPath 解析 skill 的 SKILL.md 绝对路径(技能文档的唯一解析入口)。
//
// 支持三种输入:
//   - 技能目录名(推荐,与 t_skill.skill_dir 的入库格式一致):"npa-report"
//   - 历史文档路径:"npa-report/SKILL.md"
//   - 已解析的绝对路径(调用链内部产物):必须位于 skills 根内且文件名为 SKILL.md
//
// 安全:结果的真实路径必须落在 skills 根内(防目录穿越/符号链接逃逸)。
func ResolveSkillDocPath(skillDir string) (string, error) {
	skillRoot, _ := BuildSkillRoot()

	cfg := strings.TrimSpace(skillDir)
	if len(cfg) == 0 {
		return pCommon.BlankString, pException.ParameterEmptyErrorEx("skill dir is empty")
	}

	// 根的真实路径只解析一次、两条分支共用:
	//   - 让两条分支的"越界"基准同源(不再一处传未解析根、一处走又解析一次的内部逻辑);
	//   - 也省掉目录名分支里 SafeJoin 的那次重复根解析。
	realRoot := pCommon.ResolveRealPath(skillRoot)

	// 已解析的绝对路径:只校验"仍在 skills 根内 + 名为 SKILL.md"
	if filepath.IsAbs(cfg) {
		absPath, err := pCommon.SafeJoinWithRoot(realRoot, cfg)
		if err != nil {
			return pCommon.BlankString, pException.FilePathErrorEx("skill doc escapes skill root: " + skillDir)
		}
		if filepath.Base(absPath) != SkillDocFileName {
			return pCommon.BlankString, pException.FilePathErrorEx("skill doc must be " + SkillDocFileName + ": " + skillDir)
		}
		return absPath, nil
	}

	dir, err := NormalizeSkillDir(cfg)
	if err != nil {
		return pCommon.BlankString, err
	}

	// 目录名已是安全相对路径,仍走真实路径包含性校验兜底符号链接逃逸
	absPath, err := pCommon.SafeJoinWithRoot(realRoot, filepath.Join(skillRoot, dir, SkillDocFileName))
	if err != nil {
		return pCommon.BlankString, pException.FilePathErrorEx("skill doc escapes skill root: " + skillDir)
	}

	return absPath, nil
}

// maintenanceSlots 后台记忆维护的全局并发闸(压缩/蒸馏都要调用 LLM),即最大的goroutine个数
var goroutineBarriers = make(chan struct{}, hCommon.CompactionMaxConcurrent)

// AcquireGoroutineBarrier 非阻塞获取一个维护名额(拿不到就放弃本次)。
func AcquireGoroutineBarrier() bool {
	select {
	case goroutineBarriers <- struct{}{}:
		return true
	default:
		return false
	}
}

// ReleaseGoroutineBarrier 释放维护名额。
func ReleaseGoroutineBarrier() {
	select {
	case <-goroutineBarriers:
	default:
	}
}

// IsKindAllowedInScope 按会话范围过滤启动文档(参考 OpenClaw filterBootstrapFilesForSession)。
//   - normal:   注入全部(含 MEMORY.md)
//   - subagent: 仅 AGENTS.md(不继承父 agent 的人格/用户/记忆)
//   - cron:     AGENTS/SOUL/IDENTITY/USER,不含 MEMORY.md
//   - heartbeat: 不注入启动文件
func IsKindAllowedInScope(kind hCommon.AgentDocumentKind, scope hCommon.BootstrapScope) bool {
	switch scope {
	case hCommon.BootstrapScopeSubagent:
		return kind == hCommon.AgentDocAgent
	case hCommon.BootstrapScopeCron:
		switch kind {
		case hCommon.AgentDocAgent, hCommon.AgentDocSoul,
			hCommon.AgentDocIdentity, hCommon.AgentDocUser:
			return true
		}
		return false
	case hCommon.BootstrapScopeHeartbeat:
		return false
	default:
		return true
	}
}

func ReadDocFile(filePath string, maxSize int64) DocFileSnapshot {
	snapshot := DocFileSnapshot{FilePath: filePath, State: FileMissing}

	// 类型守卫必须在**打开之前**:syscall.Open(O_RDONLY) 遇到 FIFO 会阻塞到有写端
	info, err := os.Lstat(filePath)
	if err != nil {
		snapshot.Reason = err.Error()
		return snapshot
	}
	if !info.Mode().IsRegular() {
		snapshot.Reason = "not a regular file"
		return snapshot
	}

	f, stat, err := pCommon.OpenFileNoFollow(filePath)
	if err != nil {
		snapshot.Reason = err.Error()
		return snapshot
	}
	defer func() { _ = f.Close() }()

	// 二次类型校验:上一步 Lstat 与本次 Open 之间仍可能被换成 FIFO/设备节点。
	// OpenFileNoFollow 已带 O_NONBLOCK,此处只负责给出准确的拒绝原因
	// (不再依赖"读时 EAGAIN"这种含混信号)。
	if !stat.Mode().IsRegular() {
		snapshot.Reason = "not a regular file"
		return snapshot
	}

	if stat.Size() > maxSize {
		snapshot.State = FileUnavailable
		snapshot.ByteSize = stat.Size()
		snapshot.ModTime = stat.ModTime().UnixNano()
		snapshot.Reason = fmt.Sprintf("file too large (size = %d > limit = %d)", stat.Size(), maxSize)
		return snapshot
	}

	data, err := io.ReadAll(io.LimitReader(f, maxSize+1))
	if err != nil {
		snapshot.State = FileUnavailable
		snapshot.ModTime = stat.ModTime().UnixNano()
		snapshot.Reason = err.Error()
		return snapshot
	}

	if int64(len(data)) > maxSize {
		snapshot.State = FileUnavailable
		snapshot.ModTime = stat.ModTime().UnixNano()
		snapshot.Reason = fmt.Sprintf("file too large (size = %d > limit = %d)", int64(len(data)), maxSize)
		return snapshot
	}

	snapshot.State = FileReady
	snapshot.ByteSize = int64(len(data))
	snapshot.ModTime = stat.ModTime().UnixNano()
	snapshot.Content = string(data)
	snapshot.Fingerprint = pCommon.ComputeFileFingerprint2(stat) // 同句柄、同格式
	return snapshot
}

func ParseSystemToolSchema(snapshot DocFileSnapshot, scriptFile string, visible bool) (*hCommon.SystemToolSchemaDocument, error) {
	var schemaMeta hCommon.SystemToolSchemaMeta

	if err := json.Unmarshal([]byte(snapshot.Content), &schemaMeta); err != nil {
		return nil, fmt.Errorf("invalid schema json: %w", err)
	}

	name := strings.TrimSpace(schemaMeta.Name)
	if len(name) == 0 || len(schemaMeta.InputSchema) == 0 {
		return nil, fmt.Errorf("invalid schema name: %s", schemaMeta.Name)
	}

	return &hCommon.SystemToolSchemaDocument{
		ToolName:          name,
		Visible:           visible,
		Script:            scriptFile,
		SchemaFileName:    snapshot.FilePath,
		SchemaFingerprint: snapshot.Fingerprint,
		ByteSize:          snapshot.ByteSize,
		SysToolSchemaMeta: schemaMeta,
	}, nil
}

func ParseSkillDocument(snapshot DocFileSnapshot) *hCommon.SkillDocument {
	doc := &hCommon.SkillDocument{
		SkillId:     filepath.Base(filepath.Dir(snapshot.FilePath)), //技能目录名
		SkillFile:   snapshot.FilePath,
		Fingerprint: snapshot.Fingerprint,
		ByteSize:    snapshot.ByteSize,
	}

	frontMatter, content := SplitFrontMatter([]byte(snapshot.Content))
	doc.Content = content
	if len(frontMatter) > 0 {
		if err := yaml.Unmarshal([]byte(frontMatter), &doc.FrontMatter); err != nil {
			logging.LogWarnF("prompt repo: parse skill %s front matter error:%s", snapshot.FilePath, err.Error())
		}
	}

	return doc
}

// FrontMatterDelimiter front matter 的定界符
const FrontMatterDelimiter = "---"

// SplitFrontMatter 把 markdown 文档拆分成 (frontMatter, content) 两部分。
// 只有当文档以 "---" 行开头并且存在闭合的 "---" 行时才认为存在 front matter;
// 否则返回空 frontMatter 与完整原文,保证普通 markdown 文档的正文不丢失。
// 兼容 CRLF(\r\n) 与 BOM 前缀。
func SplitFrontMatter(data []byte) (string, string) {
	// 统一行尾,兼容 Windows 编辑保存的文档
	text := strings.ReplaceAll(string(data), "\r\n", "\n")

	// 去掉 BOM 与行首空白后,必须仍以 --- 开头
	trimmed := strings.TrimLeft(text, "\ufeff \t\r\n")
	if !strings.HasPrefix(trimmed, FrontMatterDelimiter+"\n") {
		return "", text
	}

	rest := strings.TrimPrefix(trimmed[len(FrontMatterDelimiter):], "\n")

	// 查找闭合的 --- 行;未闭合则视为普通 markdown,正文为完整原文
	closeIdx := strings.Index(rest, "\n"+FrontMatterDelimiter)
	if closeIdx == -1 {
		return "", text
	}

	frontMatter := rest[:closeIdx]
	after := rest[closeIdx+len("\n"+FrontMatterDelimiter):]

	return strings.TrimSpace(frontMatter), strings.TrimPrefix(after, "\n")
}

// HasFrontMatter 判断文档是否携带 YAML front matter(以 --- 行开头且有闭合 --- 行)
func HasFrontMatter(data []byte) bool {
	frontMatter, _ := SplitFrontMatter(data)
	return len(frontMatter) > 0
}

// StripFrontMatter 返回剥离 front matter 之后的正文(与 OpenClaw stripFrontMatter 语义一致)
func StripFrontMatter(data []byte) string {
	_, content := SplitFrontMatter(data)
	return content
}

func BuildSessionName(message string) string {
	name := strings.TrimSpace(pCommon.TruncateRunes(strings.TrimSpace(message), SessionNameMaxLen, pCommon.BlankString))
	if name == pCommon.BlankString {
		return SessionDefaultName
	}
	return name
}

func BuildMemoryRoot(sender string) string {
	sender = strings.TrimSpace(sender)

	memoryRoot := filepath.Join(pCommon.GetExecutableDir(), hCommon.AgentBootstrapDir, ChatMemoryDirName)
	memoryDir, _ := pCommon.SafeJoin(memoryRoot, sender)

	_ = pCommon.CreateDir(memoryDir)

	return memoryDir
}

// BuildMemoryFileName 构造会话历史文件名:memory_{yyyyMMddHHmmss}.md
// 与 DDL 注释中的 "memory_{datetime}.md" 保持一致。
func BuildMemoryFileName(sender, sessionId string, at *pCommon.DatetimeItem) (string, string) {
	//datetime := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime()
	memoryRoot := BuildMemoryRoot(sender)

	var datetime = &pCommon.DatetimeItem{}
	if at != nil {
		datetime = at
	}

	memoryFileName := ChatMemoryFilePrefix + sessionId + FormatCompactDatetime(datetime) + ChatMemoryFileExt
	return memoryRoot, memoryFileName
}

// BuildHistoryLockKey 历史写入的会话级锁键。
// 刻意加 "history:" 前缀与服务层的会话锁区分:二者是不同临界区,
// 且 AppendTurn 已持有 <sessionId> 会话锁,若这里复用同一键会自锁。
func BuildHistoryLockKey(sessionId string) string {
	return "history:" + strings.TrimSpace(sessionId)
}

// BuildCompactionLockKey 压缩临界区的会话级锁键。
// 同样必须与 history 锁键区分:压缩的临界区内部会调用历史写入(Append),
// 而 Append 自己会取 history 锁 —— 复用同键必然自锁。
func BuildCompactionLockKey(sessionId string) string {
	return "compact:" + strings.TrimSpace(sessionId)
}

// BuildMemoryDocLockKey 长期记忆文件(MEMORY.md)的写锁键。
// 粒度是**文件路径**:同一 Agent 的多次蒸馏必须串行(读-比对-写),
// 不同 Agent 之间互不阻塞;键名同样刻意与 history/compact 区分。
func BuildMemoryDocLockKey(path string) string {
	return "memorydoc:" + strings.TrimSpace(path)
}

// FormatCompactDatetime 把时间格式化为 yyyyMMddHHmmss(用于文件名)。
func FormatCompactDatetime(at *pCommon.DatetimeItem) string {
	if at == nil {
		return pCommon.BlankString
	}
	return fmt.Sprintf("%04d%02d%02d%02d%02d%02d", at.Year, at.Month, at.Day, at.Hour, at.Minute, at.Second)
}

// BuildTurnStreamKey 构造"进行中轮次"的注册键/中断键:<sender>:<turnId>。
func BuildTurnStreamKey(sender, turnId string) string {
	return strings.TrimSpace(sender) + ":" + strings.TrimSpace(turnId)
}

// ============== private function & methods for module ==========================
// defaultContextPolicy 返回默认策略(未配置任何压缩参数时的安全值)。
func defaultContextPolicy() hCommon.ContextPolicy {
	return hCommon.ContextPolicy{
		OutputReserve:      DefaultOutputReserveTokens,
		ToolReplayMaxRunes: DefaultToolReplayMaxRunes,
	}
}
