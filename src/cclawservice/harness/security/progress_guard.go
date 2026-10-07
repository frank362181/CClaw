// Package security /*
/**************************************************************
* 无进展检测
* 职责：
*	补齐LoopGuard 中目前 5 条"重复模式"规则之外的盲区
*	模型每一步的 工具名/参数/结果 都不同（因此不会命中任何重复规则）,但整体产生的**新消息**并不会推进任务
* 判断：
*	最近 W次成功调用中,连续K次的结果签名都已在窗口中出现过(0 新消息)
* 设计约束：
*	- 启发式而非精确判断:阈值取保守值(宁可漏判,不可误杀合法长任务)
*	- 结果签名必须先 **归一化**（剥离时间戳、UUID、长数字、空白等）,否则"换参数不换结果"测不到
*	- 只有"成功且足够长"的结果参数与判定;失败与短结果(ACK)一律忽略;
*	- 窗口有界（滑动窗口），旧结果会老化，避免长任务回头读同一文件累积误报；
*	- 与LoopGuard相同:一轮对话一个实例,内部无锁,只能在发起该轮的goroutine内使用
**************************************************************/

package security

import (
	"regexp"
	"strings"
	"unicode/utf8"

	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/common/hash"
)

// 阈值(保守取值:宁可漏判、不可误杀)
const (
	progressWindowSize     = 12  //记忆窗口:只保留最近N次成功调用的结果签名
	progressMaxNoNewCalls  = 8   // 连续N次成功调用的结果都无消息 -> 判定无进展
	progressMaxResultRunes = 512 // 参与签名的结果前缀长度;过长仅取前缀
	progressMinResultRunes = 8   // 短于该长度的结果不参与判定,视为ACK

	// noProgressEnabled 是否启用规则 6(无进展检测)。当前固定开启:
	// 置 false 可整包关闭该规则(灰度观察 / 快速回滚),无需改动其余逻辑。
	noProgressEnabled = true
)

// 易变内容的正则表达式
var (
	reUUID       = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	reTimestamp  = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?`)
	reDateTimeCN = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[ T]?\d{2}:\d{2}(?::\d{2})?`)
	reLongNumber = regexp.MustCompile(`\d{6,}`)
	reInlineGap  = regexp.MustCompile(`[ \t\r\f\v]+`)
)

// ProgressVerdict 一次无进展判断
type ProgressVerdict struct {
	Stalled          bool   //是否判断为无进展
	ConsecutiveNoNew int    //当前连续无新信息的次数
	RepeatSig        string //反复出现的签名短前缀(脱敏,便于日志检索)
}

type progressTracker struct {
	window []string            //最近N次成功调用的结果签名(按时间升序)
	seen   map[string]struct{} // window的集合视图,用来判断是否重复 0(1)
	noNew  int                 //连续无新消息的次数
}

func newProgressTracker() *progressTracker {
	return &progressTracker{
		window: make([]string, 0, progressWindowSize),
		seen:   make(map[string]struct{}, progressWindowSize),
	}
}

// Observe 记录一次 **成功** 的调用结果,返回判决
// 状态语义:与LoopGuard一致:先更新再判决
//
//	 -结果过短 -> 中性(既不计入无新消息,也不清零):无法证明有新消息,也不该被当作有新进展
//		-结果已经在窗口中 -> noNew++
//		-出现新签名 -> noNew清零,并压入窗口(满则淘汰最旧的)
func (p *progressTracker) Observe(result string) ProgressVerdict {
	sig := ResultSignatureForNoProgress(result)
	if sig == pCommon.BlankString {
		return ProgressVerdict{} //短结果不参与判断
	}

	_, exists := p.seen[sig]
	if exists {
		p.noNew++
		p.touch(sig) // 刷新到新窗口的尾部:周期略大于窗口的重复消息也能重复
	} else {
		p.noNew = 0
		p.push(sig)
	}

	return ProgressVerdict{
		Stalled:          p.noNew >= progressMaxNoNewCalls,
		ConsecutiveNoNew: p.noNew,
		RepeatSig:        pCommon.TruncateRunes(sig, progressWindowSize, pCommon.BlankString),
	}
}

// 压入新签名,窗口满时淘汰旧的
func (p *progressTracker) push(sig string) {
	if len(p.window) >= progressWindowSize {
		old := p.window[0]
		p.window = p.window[1:]
		delete(p.seen, old)
	}

	p.window = append(p.window, sig)
	p.seen[sig] = struct{}{}
}

// 把已存在的签名移动到窗口尾部(保持窗口大小不变)
func (p *progressTracker) touch(sig string) {
	for i, s := range p.window {
		if s == sig {
			p.window = append(p.window[:i], p.window[i+1:]...)
			p.window = append(p.window, sig)
			return
		}
	}
}

func ResultSignatureForNoProgress(result string) string {
	normalized := NormalizeResultForProgress(result)
	if utf8.RuneCountInString(normalized) < progressMinResultRunes {
		return pCommon.BlankString
	}

	sig, err := hash.SHA256(normalized)
	if err != nil {
		return normalized
	}

	return sig
}

func NormalizeResultForProgress(result string) string {
	s := strings.TrimSpace(result)
	s = pCommon.TruncateRunes(s, progressMaxResultRunes, pCommon.BlankString)
	s = pCommon.ReplaceStringWithRegexp(s, reUUID, "<uuid>")
	s = pCommon.ReplaceStringWithRegexp(s, reTimestamp, "<ts>")
	s = pCommon.ReplaceStringWithRegexp(s, reDateTimeCN, "<ts>")
	s = pCommon.ReplaceStringWithRegexp(s, reLongNumber, "<n>")
	s = pCommon.ReplaceStringWithRegexp(s, reInlineGap, " ")
	s = strings.ToLower(s)
	return s
}
