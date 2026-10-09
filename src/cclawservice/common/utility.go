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
	"regexp"
	"strings"

	"my.favorite/cclawservice/harness/api/llmproto"
	hCommon "my.favorite/cclawservice/harness/common"
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

	if ok := pCommon.ValidateUrl(p.BaseUrl, nil); !ok {
		return pException.ParameterEmptyErrorEx("provider base url is invalid")
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
	if _, err := hCommon.NormalizeSkillDir(sk.SkillDir); err != nil {
		return err
	}
	if !dataobject.SkillScope(sk.SkillScope).Valid() {
		return pException.ParameterEmptyErrorEx("skill scope is invalid")
	}
	return nil
}

// ===================== session 业务表示部分函数===========================

// CompactIdLen 会话/轮次业务主键长度上限,与 DDL 中 session_id / turn_id 的 VARCHAR(40) 对齐。
// 长度需覆盖两类来源:
//   - 客户端传入:标准 UUID 36 字符去分隔符后为 32 字符;
//   - 服务端生成:NewCompactId(comCode) 为"单位编码(最长 16)+ UUID",上限 16+32=48,规整时截断到本值。
const CompactIdLen = 40

// sessionIdUnsafeChars 业务标识中不允许出现的字符(仅保留字母/数字/下划线,保证可安全用作目录名)。
var sessionIdUnsafeChars = regexp.MustCompile(`[^0-9a-zA-Z_]`)

// NormalizeCompactId 把外部传入的业务标识规整为可安全落库/落盘的紧凑形式:
//  1. 去首尾空白;
//  2. 去掉 '-' 分隔符(标准 UUID 36 → 32 字符);
//  3. 过滤其余非 [0-9a-zA-Z_] 字符(防路径穿越/非法字符);
//  4. 超长截断(兜底,正常路径不会触发)。
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

// ClassifyInterrupt 归类"本轮为什么没正常结束"(落库到 t_turn.interrupt_reason,日志共用同一判定)。
//
// 三个上下文分工不同,缺一不可 —— 这是唯一能区分"用户点中断"与"客户端断开"的地方:
//   - reqCtx   :请求上下文。客户端断开 → HTTP 层先取消它;
//   - runCtx   :本轮运行上下文(WithTimeout(streamCtx))。运行超时 → DeadlineExceeded;
//   - streamCtx:注册给中断 API 的上下文。用户点中断 → 它被取消,**而 reqCtx 仍然存活**。
//
// 判定顺序即优先级:超时 > 客户端断开 > 用户中断 > 上游取消 > 其它错误。
// 同步路径(SendMessage)没有 streamCtx、也没有运行超时,传 nil 即可。
func ClassifyInterrupt(reqCtx, runCtx, streamCtx context.Context, err error) dataobject.InterruptReason {
	switch {
	case runCtx != nil && errors.Is(runCtx.Err(), context.DeadlineExceeded):
		return dataobject.InterruptTimeout
	case reqCtx != nil && reqCtx.Err() != nil:
		return dataobject.InterruptByClient
	case streamCtx != nil && errors.Is(streamCtx.Err(), context.Canceled):
		return dataobject.InterruptByUser
	case err != nil && errors.Is(err, llmproto.ErrRequestCanceled):
		return dataobject.InterruptUpstream
	default:
		return dataobject.InterruptOther
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
