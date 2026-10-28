package api

import (
	"bufio"
	"io"
	"strings"

	pCommon "my.favorite/platformutility/common"
)

// 顺序读取 SSE 事件
// 只做一件事:把 event:/data:/空行 三行协议解析成 (name,data)
// 全仓唯一 **SSE 分词实现
type sseDecoder struct {
	sc     *bufio.Scanner
	event  string
	onLine func() // 每读到一行回调:重置空闲计时 / 感知 ctx 取消
}

func newSSEDecoder(r io.Reader, maxLine int, onLine func()) *sseDecoder {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), maxLine) //限制单行上限,防止过长耗尽内存
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
