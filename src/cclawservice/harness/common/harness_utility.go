/**
* harness包的工具函数
**/

package common

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"my.favorite/cclawservice/llmproto"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
	pException "my.favorite/platformutility/exception"
)

// BuildAgentBootstrapRoot 根据agent的信息构建agent的工作目录
// 安全:agent_dir 必须为合法相对路径,防止工作区根逃出 <exeDir>/workspace。
func BuildAgentBootstrapRoot(agent dataobject.TAgent) (string, bool) {
	if !pCommon.IsSafeDir(agent.AgentDir) {
		return pCommon.BlankString, false
	}

	agentBootstrapRoot := filepath.Join(pCommon.GetExecutableDir(), AgentBootstrapDir, agent.OwnerId, agent.AgentDir)
	if pCommon.IsDir(agentBootstrapRoot) {
		return agentBootstrapRoot, true
	}

	return pCommon.BlankString, false
}

// ResolveStartupPath 校验并解析启动文档路径:仅允许纯文件名,禁止目录穿越与符号链接逃逸。
// 供 prompt 组装(LoadWorkspace)与启动文件管理(StartupFileRepository)共用,
// 保证同一套安全路径规则。文件名非法/越界时返回平台 FilePathError。
func ResolveStartupPath(workspaceRoot, fileName string) (string, error) {
	// 只允许纯文件名:不能包含路径分隔符、"." 或 ".."
	if filepath.Base(fileName) != fileName || fileName == "." || fileName == ".." {
		return pCommon.BlankString, pException.FilePathErrorEx("illegal document file name: " + fileName)
	}

	return pCommon.SafeJoin(workspaceRoot, filepath.Join(workspaceRoot, fileName))
}

// BuildSkillRoot 返回技能根目录 <exeDir>/cclaw/skills。
// 所有技能都位于该目录之下,一个子目录一个技能(目录名即 t_skill.skill_dir)。
func BuildSkillRoot() (string, bool) {
	skillRoot := filepath.Join(pCommon.GetExecutableDir(), AgentBootstrapDir, SkillDirRoot)
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

// maintenanceSlots 后台记忆维护的全局并发闸(压缩/蒸馏都要调用 LLM)。
var maintenanceSlots = make(chan struct{}, CompactionMaxConcurrent)

// AcquireMaintenanceSlot 非阻塞获取一个维护名额(拿不到就放弃本次)。
func AcquireMaintenanceSlot() bool {
	select {
	case maintenanceSlots <- struct{}{}:
		return true
	default:
		return false
	}
}

// ReleaseMaintenanceSlot 释放维护名额。
func ReleaseMaintenanceSlot() {
	select {
	case <-maintenanceSlots:
	default:
	}
}

// IsKindAllowedInScope 按会话范围过滤启动文档(参考 OpenClaw filterBootstrapFilesForSession)。
//   - normal:   注入全部(含 MEMORY.md)
//   - subagent: 仅 AGENTS.md(不继承父 agent 的人格/用户/记忆)
//   - cron:     AGENTS/SOUL/IDENTITY/USER,不含 MEMORY.md
//   - heartbeat: 不注入启动文件
func IsKindAllowedInScope(kind AgentDocumentKind, scope dataobject.BootstrapScope) bool {
	switch scope {
	case dataobject.BootstrapScopeSubagent:
		return kind == AgentDocAgent
	case dataobject.BootstrapScopeCron:
		switch kind {
		case AgentDocAgent, AgentDocSoul,
			AgentDocIdentity, AgentDocUser:
			return true
		}
		return false
	case dataobject.BootstrapScopeHeartbeat:
		return false
	default:
		return true
	}
}

func ParseJsonDocument(jsonFile string) (SystemicTool, bool) {
	return SystemicTool{}, false
}

// BuildSkillTool 把一份**已解析**的 SKILL.md 文档组装成技能级工具视图(SkillTool)。
//
// 读盘与 YAML 解析都不在这里 —— 唯一入口是 prompt repo 的 LoadSkills(带文件指纹缓存),
// 本函数只做派生:把 front matter 里声明的工具脚本解析成绝对路径(相对 SKILL.md 所在目录),
// 并挑出声明了 force-call 的工具名。
// 文档未携带文件路径(未落盘的文档)时返回 ok=false。
func BuildSkillTool(doc SkillDocument) (SkillTool, bool) {
	skillFile := strings.TrimSpace(doc.SkillFile)
	if len(skillFile) == 0 {
		return SkillTool{}, false
	}

	var out []BasicTool
	var forced []string

	for _, t := range doc.FrontMatter.Tools {
		if len(strings.TrimSpace(t.Name)) == 0 {
			continue // 工具名称为空即放弃
		}
		baseDir := filepath.Dir(skillFile)
		t.Script, _ = pCommon.ResolveFile(baseDir, t.Script)
		out = append(out, t)
		if t.ForceCall {
			forced = append(forced, strings.TrimSpace(t.Name))
		}
	}

	return SkillTool{
		BasicTool: BasicTool{
			Source:      TSourceSkill,
			Name:        strings.TrimSpace(doc.FrontMatter.Name),
			Description: strings.TrimSpace(doc.FrontMatter.Description),
		},
		Tools:            out,
		AllowedTools:     doc.FrontMatter.AllowedTools,
		DisallowedTools:  doc.FrontMatter.DisallowedTools,
		TriggerKeywords:  pCommon.NormalizeStrings(doc.FrontMatter.TriggerKeywords),
		ForcedTools:      pCommon.NormalizeStrings(forced),
		SkillFileName:    skillFile,
		SkillFingerprint: doc.Fingerprint,
	}, true
}

func BasicToolToLLMTool(t BasicTool) (llmproto.Tool, bool) {
	name := strings.TrimSpace(t.Name)
	if len(name) == 0 {
		return llmproto.Tool{}, false
	}

	return llmproto.Tool{
		Type: "function",
		Function: llmproto.ToolFunction{
			Name:        name,
			Description: strings.TrimSpace(t.Description),
			Parameters:  pCommon.YamlToJsonMap(t.Parameters),
		},
	}, true
}

// FindStartupDocFile 按文件名(精确匹配)在启动文件清单中查找,未命中返回 ok=false。
func FindStartupDocFile(fileName string) (StartupDocSpec, bool) {
	for _, f := range StartupDocFiles {
		if f.Name == fileName {
			return f, true
		}
	}
	return StartupDocSpec{}, false
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

// BuildMemoryFileName 构造会话历史文件名:memory_{yyyyMMddHHmmss}.md
// 与 DDL 注释中的 "memory_{datetime}.md" 保持一致。
func BuildMemoryFileName(at *pCommon.DatetimeItem) string {
	datetime := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime()
	if at != nil {
		datetime = at
	}
	return ChatMemoryFilePrefix + FormatCompactDatetime(datetime) + ChatMemoryFileExt
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

// StreamReadError 归一化流读取失败:
// 流式读取失败的归一化:把底层读流错误分类为"客户端断开"或"传输层故障"。
//   - 客户端断开(外层 ctx 取消)      → ErrRequestCanceled(静默,不重试)
//   - 空闲超时(仅内层 streamCtx 取消) → 传输层错误(可重试)
//   - 其它(连接重置/EOF 等)          → 传输层错误(可重试)
func StreamReadError(ctx context.Context, streamCtx context.Context, err error, requestID string, idle time.Duration) error {
	if ctx.Err() != nil {
		return llmproto.ErrRequestCanceled
	}
	if idle > 0 && streamCtx.Err() != nil {
		return llmproto.NewLlmStreamTransportError(
			fmt.Errorf("stream idle timeout after %s without data: %v", idle, err), requestID)
	}
	if pException.IsContextCanceled(streamCtx, err) {
		return llmproto.ErrRequestCanceled
	}
	return llmproto.NewLlmStreamTransportError(err, requestID)
}
