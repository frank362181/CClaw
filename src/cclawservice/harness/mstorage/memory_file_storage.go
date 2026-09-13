/**
* 会话历史文件写入器
*
* 职责:把会话内容以"追加型记录"写入 memory_{yyyyMMddHHmmss}.md,
* 并返回每条记录在文件中的序号(index),供 t_turn.chat_index / t_turn.resp_index 引用。
*
* 与 DDL 的对应关系(data/ddl_sqlite3.sql):
*   - t_session.chat_data 只保存文件名(不含目录);目录由 session_id 推导:
*       <exeDir>/cclaw/chat_history/<session_id>/
*   - t_turn.chat_index      = 用户问题记录的序号
*   - t_turn.resp_index      = 助手回复记录的序号
*   - t_turn_tool 的完整输出也写入本文件(DB 里 tool_resp 只留 256 字符摘要)
*
* 文件格式(追加型,记录序号从 1 起单调递增):
*
*   # 会话历史 <sessionId>
*
*   - session_id: xxx
*   - agent_id: xxx
*   ...
*
*   ---
*
*   ## [1] user | 2026-09-12 10:00:00
*   <用户问题>
*
*   ## [2] tool:get_weather | 2026-09-12 10:00:03
*   <工具完整输出>
*
*   ## [3] assistant | 2026-09-12 10:00:05
*   <助手回复>
*
* 并发:
*   - 同一会话的"分配序号 + 追加"由本写入器按会话键(history:<sessionId>)串行化,
*     不同会话互不阻塞(旧实现用写入器级全局锁,一次冷扫描会阻塞所有会话);
*   - 刻意不复用服务层的会话锁键:AppendTurn 已持有 <sessionId>,复用即自锁;
*   - nextIndex 缓存有容量上限,超出整体重置,防止随会话数无限膨胀。
**/

package mstorage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	myCommon "my.favorite/cclawservice/common"
	hCommon "my.favorite/cclawservice/harness/common"
	pCommon "my.favorite/platformutility/common"
	pException "my.favorite/platformutility/exception"
)

// 历史记录类型(写入 markdown 的记录头)
const (
	HistoryRecordUser      = "user"      // 用户问题
	HistoryRecordAssistant = "assistant" // 助手回复
	HistoryRecordTool      = "tool"      // 工具调用
)

// memoryRecordHeaderString 匹配记录头 "## [<n>] ...",用于恢复下一条记录序号。
var memoryRecordHeaderString = `(?m)^## \[(\d+)\]`

// MemoryFileRWriter 会话历史文件读写器(线程安全)。
type MemoryFileRWriter struct {
	MemoryStorage

	mu         sync.Mutex
	nextIndex  map[string]int // 历史文件绝对路径 -> 下一条记录序号
	maxEntries int            // nextIndex 容量上限;<=0 表示不限制
}

// 写入器默认配置
const (
	defaultHistoryIndexMaxEntries = 8192 // 每个.md文件最大的turn条数

	// defaultTailScanChunkBytes 尾部反向扫描的初始分块大小。
	// 正常只需读最后一个记录所在的一块;最后一个记录更大时按 4 倍逐次放大。
	defaultTailScanChunkBytes = 64 * 1024
)

var memoryFileRWriter = pCommon.GetSingleton(func() *MemoryFileRWriter {
	return &MemoryFileRWriter{
		MemoryStorage: MemoryStorage{
			storageType: MemoryStorageMDFile,
		},
		nextIndex:  make(map[string]int),
		maxEntries: defaultHistoryIndexMaxEntries,
	}
}).Instance()

// GetMemoryFileWriter 返回全局唯一的历史文件写入器。
func GetMemoryFileWriter() IMemoryStorage { //*MemoryFileRWriter {
	return memoryFileRWriter
}

// MemoryDir 返回某会话的历史文件目录:<exeDir>/cclaw/chat_history/<session_id>。
// session_id 直接参与路径拼接,必须满足两个条件:
//  1. 是安全的相对路径(非绝对、无 . / .. 段)—— 由 IsSafeDir 保证;
//  2. 是"单一路径段"(不含任何目录分隔符)。
func (w *MemoryFileRWriter) MemoryDir(sessionId string) (string, error) {
	sessionId = strings.TrimSpace(sessionId)
	if !pCommon.IsSafeDir(sessionId) || strings.ContainsAny(sessionId, `/\`) {
		return pCommon.BlankString, pException.FilePathErrorEx("illegal session id for history dir: " + sessionId)
	}
	return filepath.Join(pCommon.GetExecutableDir(), myCommon.AgentBootstrapDir, myCommon.ChatMemoryDirName, sessionId), nil
}

// Ensure 确保会话历史文件存在并返回其文件名(不含目录)。
//   - 文件不存在时写入头部(会话标识 + 元数据行);
//   - 文件已存在时直接返回文件名,不覆盖既有内容。
func (w *MemoryFileRWriter) Ensure(sessionId string, at *pCommon.DatetimeItem, headerLines []string) (string, error) {
	fileName := hCommon.BuildMemoryFileName(at)
	path, err := w.resolvePath(sessionId, fileName)
	if err != nil {
		return pCommon.BlankString, err
	}

	// 与 Append 共用同一把会话锁:避免"判不存在"与"追加写"交错写出重复头部
	unlock := GetStorageLocks().Lock(hCommon.BuildHistoryLockKey(sessionId))
	defer unlock()

	if pCommon.FileExists(path) {
		return fileName, nil
	}

	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "# NewSession %s\n\n", sessionId)
	for _, line := range headerLines {
		line = strings.TrimSpace(line)
		if line == pCommon.BlankString {
			continue
		}
		sb.WriteString("- ")
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("\n---\n\n")

	if err := w.appendToFile(path, sb.String()); err != nil {
		return pCommon.BlankString, err
	}
	return fileName, nil
}

// Append 追加一条记录,返回该记录在文件中的序号(从 1 开始)。
//   - recordType:见 HistoryRecord*;
//   - title:      可选,工具记录传工具名(记录头形如 "## [2] tool:get_weather");
//   - content:    记录正文(用户问题/助手回复/工具完整输出);
//   - timeText:   记录头时间文本(yyyy-MM-dd HH:mm:ss),为空则取当前时间;
//   - hintIndex:  调用方给出的期望序号(权威来源:DB 的 MAX(resp_index)+1);
//     <=0 表示未提供。
//
// 序号取值顺序(三者取最大值,保证契约里的"只增不减"):
//  1. 本进程已分配到的下一个序号(缓存);
//  2. hintIndex —— DB 是序号的权威来源,比缓存新(如压缩重写后)时以它为准;
//  3. 兜底:从文件末尾反向扫描最后一个记录头。
//
// 取最大值同时满足两个目标:DB 更新时立即跟上,DB 陈旧时也不会回退。
func (w *MemoryFileRWriter) Append(sessionId, fileName, recordType, title, content, timeText string, hintIndex int) (int, error) {
	path, err := w.resolvePath(sessionId, fileName)
	if err != nil {
		return 0, err
	}

	header := recordType
	if t := strings.TrimSpace(title); t != pCommon.BlankString {
		header = recordType + ":" + t
	}

	if strings.TrimSpace(timeText) == pCommon.BlankString {
		timeText = pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().DatetimeStr
	}

	// 会话级串行:同一会话内"分配序号 + 追加写"必须原子;不同会话互不阻塞。
	// 锁在磁盘 IO 与尾部扫描期间保持——但只阻塞同一会话。
	unlock := GetStorageLocks().Lock(hCommon.BuildHistoryLockKey(sessionId))
	defer unlock()

	index := w.nextIndexFor(path, hintIndex)

	block := fmt.Sprintf("## [%d] %s | %s\n\n%s\n\n", index, header, timeText, strings.TrimSpace(content))
	if err := w.appendToFile(path, block); err != nil {
		return 0, err
	}

	w.setNextIndex(path, index+1)
	return index, nil
}

// nextIndexFor 解析本次写入应使用的序号(详见 Append 的序号取值顺序)。
// 注意:可能触发磁盘扫描,故**不**在 w.mu 内调用。
func (w *MemoryFileRWriter) nextIndexFor(path string, hintIndex int) int {
	w.mu.Lock()
	index := w.nextIndex[path]
	w.mu.Unlock()

	if hintIndex > index {
		index = hintIndex // DB 是权威来源:比本进程缓存新时以它为准
	}
	if index <= 0 {
		index = scanTailNextRecordIndex(path) // 冷启动/缓存缺失:仅扫尾部
	}
	return index
}

// setNextIndex 记录下一个可用序号;缓存满时整体重置,保证内存有界。
func (w *MemoryFileRWriter) setNextIndex(path string, next int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.maxEntries > 0 && len(w.nextIndex) >= w.maxEntries {
		w.nextIndex = make(map[string]int)
	}
	w.nextIndex[path] = next
}

// ============================== private functions & methods ==============================

// resolvePath 把(会话标识, 文件名)解析为历史文件绝对路径,并确保目录存在。
// 文件名必须是纯文件名,防止穿越出会话目录。
func (w *MemoryFileRWriter) resolvePath(sessionId, fileName string) (string, error) {
	dir, err := w.MemoryDir(sessionId)
	if err != nil {
		return pCommon.BlankString, err
	}

	fileName = strings.TrimSpace(fileName)
	if fileName == pCommon.BlankString || filepath.Base(fileName) != fileName {
		return pCommon.BlankString, pException.FilePathErrorEx("illegal history file name: " + fileName)
	}

	if err := pCommon.CreateDir(dir); err != nil {
		return pCommon.BlankString, pException.FilePathErrorEx("create history dir error: " + err.Error())
	}
	return filepath.Join(dir, fileName), nil
}

// appendToFile 以追加方式写入文本;文件不存在则创建。
func (w *MemoryFileRWriter) appendToFile(path, text string) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return pException.FilePathErrorEx("open history file error: " + err.Error())
	}
	defer func() { _ = file.Close() }()

	if _, err = file.WriteString(text); err != nil {
		return pException.FilePathErrorEx("write history file error: " + err.Error())
	}
	return nil
}

// scanTailNextRecordIndex 从**文件末尾反向分块**查找最后一个记录头,返回 max(n)+1;无记录返回 1。
// 反向分块扫描的正常成本只是"最后一个记录所在的那一块";
// 最后一个记录本身很大时块大小按 4 倍逐次放大,最坏才退化到全文件,
// 但始终不整体载入内存、不做全文正则。
func scanTailNextRecordIndex(path string) int {
	file, err := os.Open(path)
	if err != nil {
		return 1 // 文件缺失/不可读:按"无记录"处理,由后续写入自行创建
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil || info.Size() <= 0 {
		return 1
	}

	size := info.Size()
	chunk := int64(defaultTailScanChunkBytes)
	if chunk > size {
		chunk = size
	}

	for {
		start := size - chunk
		if start < 0 {
			start = 0
		}

		buf := make([]byte, chunk)
		n, readErr := file.ReadAt(buf, start)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return 1
		}
		if n > 0 {
			// 本块覆盖 [start, EOF):只要最后一个记录头不在块首被截断,这里的最大值就是全局最大值
			if index := pCommon.GetIntFromRegex(memoryRecordHeaderString, buf[:n]); index > 0 { //maxRecordIndexIn(buf[:n]); index > 0 {
				return index + 1
			}
		}

		if start == 0 {
			return 1
		}
		if chunk *= 4; chunk > size {
			chunk = size
		}
	}
}
