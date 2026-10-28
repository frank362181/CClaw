package chat

import (
	"bufio"
	"io"
	"strings"

	"my.favorite/cclawservice/exception"
	hCommon "my.favorite/cclawservice/harness/common"
	pCommon "my.favorite/platformutility/common"
)

// bufferReader 给响应体加"累计字节"上限。
//
// 为什么不用 io.LimitReader:LimitReader 超限后返回 EOF,bufio.Scanner 会把它当成
// "流正常结束",上层可能把半截回答当完整回答。这里超限返回明确错误,
// 由 hCommon.StreamReadError 分类为确定性失败(不重试)。
type bufferReader struct {
	reader     io.Reader
	limit      int64 // 剩余可读字节数量
	totalLimit int64 // 构造时的原始上限(仅用于错误文案:limit 会被递减到 0)
}

// newLimitedReader limit<=0 表示不限制(直接返回原 reader,零开销)。
func newLimitedReader(r io.Reader, limit int64) io.Reader {
	if limit <= 0 {
		return r
	}

	return &bufferReader{
		reader:     r,
		limit:      limit,
		totalLimit: limit,
	}
}

func (r *bufferReader) Read(p []byte) (n int, err error) {
	if r.limit <= 0 {
		// 额度已经用尽:确认上游是否真的还有数据 -- 恰好等于上限的响应此时已经EOF
		// 不能误报超限
		var probe [1]byte
		n, err := r.reader.Read(probe[:])
		if n == 0 {
			return 0, err //err可能是 io.EOF(正常结束)
		}
		return 0, exception.ChatResponseTooLargeKind.Newf(
			"transport: response exceeds max bytes limit (%d bytes)", r.totalLimit)
	}

	if int64(len(p)) > r.limit {
		p = p[:r.limit] // 只读到这里为止
	}

	n, err = r.reader.Read(p)
	r.limit -= int64(n)

	return n, err
}

// 顺序读取 SSE 事件
// 只做一件事:把 event:/data:/空行 三行协议解析成 (name,data)
// 全仓唯一 **SSE 分词实现
type sseDecoder struct {
	sc     *bufio.Scanner
	event  string
	onLine func() // 每读到一行回调:重置空闲计时 / 感知 ctx 取消
}

// 小初始化 + 大上限:
//   - DefaultSSELineBytes(64KB) 是初始 buffer,等于每连接的常驻内存
//     (Scanner 的 buf 只增不减,所以初始值要小);
//   - maxLine(MaxSSELineBytes) 才是单行上限,遇到超长行时才会扩容。
func newSSEDecoder(r io.Reader, maxLine int, onLine func()) *sseDecoder {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, hCommon.DefaultSSELineBytes), maxLine)
	return &sseDecoder{
		sc:     sc,
		onLine: onLine,
	}
}

func (sse *sseDecoder) Next() (string, string, bool) {
	for sse.sc.Scan() {
		if sse.onLine != nil {
			sse.onLine()
		}

		line := strings.TrimSpace(sse.sc.Text())
		switch {
		case line == pCommon.BlankString:
			sse.event = pCommon.BlankString // 事件块结束
		case strings.HasPrefix(line, "event:"):
			sse.event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			return sse.event, strings.TrimSpace(strings.TrimPrefix(line, "data:")), true
		}
	}

	return "", "", false
}

func (sse *sseDecoder) Error() error {
	return sse.sc.Err()
}
