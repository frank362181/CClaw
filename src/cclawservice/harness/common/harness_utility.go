/**
* harness包的工具函数
**/

package common

import (
	"context"
	"fmt"
	"strings"
	"time"

	"my.favorite/cclawservice/llmproto"
	pCommon "my.favorite/platformutility/common"
	pException "my.favorite/platformutility/exception"
)

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
