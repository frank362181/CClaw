/**
* 通用功能函数
*
* 约定:文档顶部的 --- 与 --- 之间为 YAML 元数据(如 name/description/summary 等),
* 其余部分为正文。本工具只负责"拆分",具体字段的解析由各强类型结构体完成。
*
* stripFrontMatter / extractFrontmatterBlock:
* 正文必须被完整保留 —— 没有 front matter 的文档,整个原文即正文。
**/

package common

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"strings"

	"my.favorite/cclawservice/llmproto"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
	pException "my.favorite/platformutility/exception"
)

func NormalizeOwner(ownerId *string) {
	if strings.TrimSpace(*ownerId) == "" {
		*ownerId = OwnerIdPlatform // 平台默认归属,与 DDL 默认值一致
	}
}

func ValidateLLMProvider(p dataobject.TLlm) error {
	if strings.TrimSpace(p.ProviderId) == "" {
		return pException.ParameterEmptyErrorEx("provider id is empty")
	}
	if strings.TrimSpace(p.ProviderName) == "" {
		return pException.ParameterEmptyErrorEx("provider name is empty")
	}
	if strings.TrimSpace(p.BaseUrl) == "" {
		return pException.ParameterEmptyErrorEx("provider base url is empty")
	}
	return nil
}

func ValidateAgent(a dataobject.TAgent) error {
	if strings.TrimSpace(a.AgentId) == "" {
		return pException.ParameterEmptyErrorEx("agent id is empty")
	}
	if strings.TrimSpace(a.AgentName) == "" {
		return pException.ParameterEmptyErrorEx("agent name is empty")
	}
	if strings.TrimSpace(a.PrimaryModel) == "" {
		return pException.ParameterEmptyErrorEx("agent primary model is empty")
	}
	if strings.TrimSpace(a.AgentDir) == "" {
		return pException.ParameterEmptyErrorEx("agent dir is empty")
	}
	if !pCommon.IsSafeDir(a.AgentDir) {
		return pException.ParameterEmptyErrorEx("agent dir is an illegal path")
	}
	if strings.TrimSpace(a.Workspace) == "" {
		return pException.ParameterEmptyErrorEx("agent workspace is empty")
	}
	return nil
}

func ValidateSkill(sk dataobject.TSkill) error {
	if strings.TrimSpace(sk.SkillId) == "" {
		return pException.ParameterEmptyErrorEx("skill id is empty")
	}
	if strings.TrimSpace(sk.SkillName) == "" {
		return pException.ParameterEmptyErrorEx("skill name is empty")
	}
	if _, err := NormalizeSkillDir(sk.SkillDir); err != nil {
		return err
	}
	if !dataobject.SkillScope(sk.SkillScope).Valid() {
		return pException.ParameterEmptyErrorEx("skill scope is invalid")
	}
	return nil
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
//   - 已解析的绝对路径(调用链内部产物,如 collectSkillFiles 的输出):
//     必须位于 skills 根内且文件名为 SKILL.md
//
// 安全:结果的真实路径必须落在 skills 根内(防目录穿越/符号链接逃逸)。
func ResolveSkillDocPath(skillDir string) (string, error) {
	skillRoot, _ := BuildSkillRoot()

	cfg := strings.TrimSpace(skillDir)
	if len(cfg) == 0 {
		return pCommon.BlankString, pException.ParameterEmptyErrorEx("skill dir is empty")
	}

	// 已解析的绝对路径:只校验"仍在 skills 根内 + 名为 SKILL.md"
	if filepath.IsAbs(cfg) {
		absPath, err := pCommon.SafeJoin(skillRoot, cfg)
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

	// 目录名已是安全相对路径,仍走 SafeJoin 兜底符号链接逃逸
	absPath, err := pCommon.SafeJoin(skillRoot, filepath.Join(skillRoot, dir, SkillDocFileName))
	if err != nil {
		return pCommon.BlankString, pException.FilePathErrorEx("skill doc escapes skill root: " + skillDir)
	}

	return absPath, nil
}

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

// BuildSkillRoot 返回技能根目录 <exeDir>/cclaw/skills。
// 所有技能都位于该目录之下,一个子目录一个技能(目录名即 t_skill.skill_dir)。
func BuildSkillRoot() (string, bool) {
	skillRoot := filepath.Join(pCommon.GetExecutableDir(), AgentBootstrapDir, SkillDirName)
	return skillRoot, true
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

// FindStartupDocFile 按文件名(精确匹配)在启动文件清单中查找,未命中返回 ok=false。
func FindStartupDocFile(fileName string) (StartupDocSpec, bool) {
	for _, f := range StartupDocFiles {
		if f.Name == fileName {
			return f, true
		}
	}
	return StartupDocSpec{}, false
}

// ===================== session 业务表示部分函数===========================

// CompactIdLen 会话/轮次业务主键长度上限,与 DDL 中 session_id / turn_id 的 VARCHAR(40) 对齐。
// 长度需覆盖两类来源:
//   - 客户端传入:标准 UUID 36 字符去分隔符后为 32 字符;
//   - 服务端生成:NewCompactId(comCode) 为"单位编码(最长 16)+ UUID",上限 16+32=48,规整时截断到本值。
const CompactIdLen = 40

// sessionIdUnsafeChars 业务标识中不允许出现的字符(仅保留字母/数字/下划线,保证可安全用作目录名)。
var sessionIdUnsafeChars = regexp.MustCompile(`[^0-9a-zA-Z_]`)

// BuildTurnStreamKey 构造"进行中轮次"的注册键/中断键:<sender>:<turnId>。
func BuildTurnStreamKey(sender, turnId string) string {
	return strings.TrimSpace(sender) + ":" + strings.TrimSpace(turnId)
}

// NormalizeCompactId 把外部传入的业务标识规整为可安全落库/落盘的紧凑形式:
//  1. 去首尾空白;
//  2. 去掉 '-' 分隔符(标准 UUID 36 → 32 字符);
//  3. 过滤其余非 [0-9a-zA-Z_] 字符(防路径穿越/非法字符);
//  4. 超长截断(兜底,正常路径不会触发)。
//
// 该函数是幂等的:NormalizeCompactId(NormalizeCompactId(x)) == NormalizeCompactId(x),
// 因此前端每次携带同一个 convId / turnId 反复调用也始终得到同一个规范值。
// 规整后为空表示入参非法,调用方应重新生成业务标识。
func NormalizeCompactId(compactId string) string {
	s := strings.TrimSpace(compactId)
	if s == pCommon.BlankString {
		return pCommon.BlankString
	}

	s = strings.ReplaceAll(s, "-", pCommon.BlankString)
	s = sessionIdUnsafeChars.ReplaceAllString(s, pCommon.BlankString)
	if len(s) > CompactIdLen {
		s = s[:CompactIdLen]
	}
	return s
}

// NewCompactId 生成紧凑唯一 ID(UUID v4 去分隔符后为 32 字符),prefix 为可选可读前缀。
//
// 前缀约定(与调用方保持一致):
//   - session_id:传 comCode(单位编码),便于按单位归档会话历史目录;
//   - turn_id:    传空串 —— turnId 必须"不含发送者",跨用户隔离统一由 BuildTurnStreamKey 承担,
//     避免 turnId 出现"有时含 sender、有时不含"的双重语义。
//
// 前缀会占用 CompactIdLen 的额度(前缀 + 32 字符 UUID 超长时截断),故前缀宜短。
func NewCompactId(prefix string) string {
	return NormalizeCompactId(prefix + pCommon.GenerateUUID())
}

// IsInterrupted 用于报告一次失败是否属于“被中断”(用户取消/客户断开/任务超时)
func IsInterrupted(runCtx context.Context, err error) bool {
	if runCtx != nil && (errors.Is(runCtx.Err(), context.Canceled) || errors.Is(runCtx.Err(), context.DeadlineExceeded)) {
		return true
	}

	return err != nil && errors.Is(err, llmproto.ErrRequestCanceled)
}

// InterruptionReason 归类中断原因
func InterruptionReason(runCtx context.Context, err error) string {
	switch {
	case runCtx != nil && errors.Is(runCtx.Err(), context.DeadlineExceeded):
		return "run timeout"
	case runCtx != nil && errors.Is(runCtx.Err(), context.Canceled):
		return "interrupted by user or client disconnected"
	case err != nil && errors.Is(err, llmproto.ErrRequestCanceled):
		return "upstream stream canceled"
	default:
		return "stream error"
	}
}

// FinishReasonNotice 把上游finish_reason映射为独立的收尾提示(code + 文案)
func FinishReasonNotice(finishReason string) (code, msg string) {
	switch strings.TrimSpace(finishReason) {
	case llmproto.FinishReasonLength:
		return ChatNoticeTruncated, ConstChatNoticeTruncatedMsg
	case llmproto.FinishReasonContentFilter:
		return ChatNoticeContentFilter, ConstChatNoticeContentFilterMsg
	default:
		return "", ""
	}
}
