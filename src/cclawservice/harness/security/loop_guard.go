/**
* 循环守卫器
*
* 职责:防止 Agent 陷入工具的死循环调用
*
* 判断条件:
*   工具总的调用次数
*   连续调用错误的次数
*   同一工具连续调用的次数
*	完全相同的调用
*   二元交替的调用(A,B,A,B...)
**/

package security

import (
	"fmt"
	"strings"

	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/common/hash"
)

// LoopGuardRule 命中的判定规则(与文件头"判断条件"一一对应;供日志/指标/分级使用)。
type LoopGuardRule int

const (
	LoopRuleNone                LoopGuardRule = iota
	LoopRuleTotalCallings                     // 1. 工具调用总量
	LoopRuleConsecutiveErrors                 // 2. 连续调用失败
	LoopRuleSameToolCallings                  // 3. 同一工具连续调用
	LoopRuleIdenticalCallings                 // 4. 完全相同的调用
	LoopRuleAlternatingCallings               // 5. 二元交替调用
	LoopRuleNoProgress                        // 无进展
)

// String 记录/指标用的稳定名称(日志里不用数字,便于检索)。
func (r LoopGuardRule) String() string {
	switch r {
	case LoopRuleTotalCallings:
		return "total_callings"
	case LoopRuleConsecutiveErrors:
		return "consecutive_errors"
	case LoopRuleSameToolCallings:
		return "same_tool_callings"
	case LoopRuleIdenticalCallings:
		return "identical_callings"
	case LoopRuleAlternatingCallings:
		return "alternating_callings"
	case LoopRuleNoProgress:
		return "no_progress"
	default:
		return "none"
	}
}

// level 该规则对应的提示级别 —— 提示语本身就分两类:
//   - Warn:只是提醒"换一种方法/方式",模型换策略就能继续;
//   - Stop:已经明确要求"停止重试/总结进度并停止"。
func (r LoopGuardRule) level() LoopGuardLevel {
	switch r {
	case LoopRuleConsecutiveErrors, LoopRuleSameToolCallings:
		return LoopLevelWarn
	case LoopRuleTotalCallings, LoopRuleIdenticalCallings, LoopRuleAlternatingCallings, LoopRuleNoProgress:
		return LoopLevelStop
	default:
		return LoopLevelNone
	}
}

// LoopGuardLevel 提示级别:决定编排层怎么处置、以及给用户的提示口径。
type LoopGuardLevel int

const (
	LoopLevelNone LoopGuardLevel = iota
	LoopLevelWarn                // 提醒换策略(可继续)
	LoopLevelStop                // 要求停止重试/收尾
)

// String 记录/指标用的稳定名称。
func (l LoopGuardLevel) String() string {
	switch l {
	case LoopLevelWarn:
		return "warn"
	case LoopLevelStop:
		return "stop"
	default:
		return "none"
	}
}

// LoopGuardVerdict 一次判定的结构化结果。未命中时为零值(Rule/Level 均为 None、Msg 为空串)。
type LoopGuardVerdict struct {
	Rule  LoopGuardRule  // 命中的规则
	Level LoopGuardLevel // 提示级别
	Msg   string         // 给模型的提示(要回填进 messages 才起作用)
}

// Hit 是否命中。
func (v LoopGuardVerdict) Hit() bool { return v.Level != LoopLevelNone }

// newLoopGuardVerdict 组装判定结果(级别由规则决定,避免两处各写一份)。
func newLoopGuardVerdict(rule LoopGuardRule, format string, args ...any) LoopGuardVerdict {
	return LoopGuardVerdict{Rule: rule, Level: rule.level(), Msg: fmt.Sprintf(format, args...)}
}

// 用来判断是否限于死循环调用的常量
const (
	maxToolTotalCallings    = 800 //一轮对话中最多调用工具的次数
	maxToolTotalCallingsStr = "工具调用总量已达 %d 次，请总结进度并停止。"

	maxConsecutiveCallErrors    = 5 //连续失败的次数
	maxConsecutiveCallErrorsStr = "已连续 %d 调用失败，请换一种方法"

	maxSameToolCallings    = 10 //同一工具连续调用的次数
	maxSameToolCallingsStr = "已经连续调用 %d 次 %s，请换一种方式。"

	maxIdenticalToolCallings   = 3 //完全相同的调用
	maxIdenticalToolCallingMsg = "检测到死循环：最近 %d 次调用了完全相同 %s。请立即停止重试，换一种完全不同的策略。"

	maxAlternatingToolCallings   = 3 //二元交替(A,B)连续调用的轮次,即 A,B,A,B,A,B 共 6 次调用
	maxAlternatingToolCallingMsg = "检测到死循环：最近 %d 轮在 %s 与 %s 之间交替调用。请立即停止重试，换一种完全不同的策略。"

	maxAlternatingSameToolCallingMsg = "检测到死循环：最近 %d 轮用同一工具 %s 在两组参数之间交替调用。请立即停止重试，换一种完全不同的策略。"

	maxNoProgressCalls = progressMaxNoNewCalls
	maxNoProgressMsg   = "检测到无进展：最近 %d 次工具调用的结果没有产生任何新信息（反复得到同一结果）。" +
		"请立即停止重试，换一种完全不同的策略，或直接总结当前已获得的结论。"

	// 调用签名(工具名 + 参数哈希 + 结果前缀)相关
	maxRecentToolCallings   = 10  //签名窗口:只保留最近几次调用的签名
	maxSignatureResultRunes = 200 //签名里保留的工具结果前缀长度(按 rune 截断,避免切坏多字节字符)
	emptyToolArgsHash       = "-" //参数为空时的签名占位:SHA256 对空输入直接报错,空串会与"真实哈希"混淆
)

// LoopGuard 一轮对话的工具调用死循环判定器。
//
// 生命周期(关键):一轮对话一个实例 —— 由调用方在工具循环内创建局部变量。
// 内部无锁、无 Reset,只能在发起该轮的 goroutine 内使用;
// 禁止做成单例/服务字段/按 agent 或会话缓存:状态跨轮或跨用户共享会串味并产生数据竞争。
type LoopGuard struct {
	curToolTotalCallings     int // 当前工具调用的总的次数
	curConsecutiveCallErrors int //当前连续失败的次数

	lastToolName        string // 上一次调用的工具的名称
	curSameToolCallings int    //当前同一工具调用的次数

	recentTools []string

	progress *progressTracker // 规则6:结果新颖度跟踪(无新进展检测)
}

// NewLoopGuard 创建一轮对话的守卫。每轮对话必须新建,禁止复用上一轮的实例。
func NewLoopGuard() *LoopGuard {
	return &LoopGuard{
		curToolTotalCallings:     0,
		curConsecutiveCallErrors: 0,
		lastToolName:             "",
		curSameToolCallings:      0,
		recentTools:              []string{},
		progress:                 newProgressTracker(),
	}
}

// Check 判定一次工具调用,返回命中的规则/级别/给模型的提示。
//
// 调用方必须按"模型声明的调用顺序"逐次调用
// (流式路径同轮多个工具是并发执行的,必须在串行回填点判定),这样"连续调用/交替调用"才有意义,也无需给守卫加锁。
//
// 状态一律"先更新、再按优先级判定":命中规则 1/2 时那次调用同样要计入「同一工具计数」与签名窗口,
// 否则编排层允许"命中后继续跑"时,后续判定会基于过期状态。
func (g *LoopGuard) Check(toolName, toolArgs string, resultOK bool, result string) LoopGuardVerdict {
	prevToolName := g.lastToolName // 上一次调用的工具的名称(下面规则 3 会覆盖)
	g.curToolTotalCallings += 1

	// 连续失败:成功即清零
	if !resultOK {
		g.curConsecutiveCallErrors += 1
	} else {
		g.curConsecutiveCallErrors = 0
	}

	// 同一工具连续调用:换工具时从 1 重新计
	if strings.EqualFold(toolName, g.lastToolName) {
		g.curSameToolCallings += 1
	} else {
		g.lastToolName = toolName
		g.curSameToolCallings = 1 //本次调用即为新工具的第1次调用
	}

	// 调用签名窗口:工具名 + 参数哈希 + 结果前缀
	argHash, err := hash.SHA256(toolArgs)
	if err != nil {
		argHash = emptyToolArgsHash // 参数为空:SHA256 直接报错,用占位符避开空白段
	}
	toolSig := toolName + ":" + argHash + ":" + pCommon.TruncateRunes(result, maxSignatureResultRunes, pCommon.BlankString)
	g.recentTools = append(g.recentTools, toolSig)
	if len(g.recentTools) > maxRecentToolCallings {
		g.recentTools = g.recentTools[len(g.recentTools)-maxRecentToolCallings:] //只保留最近的几个
	}

	var noProgress ProgressVerdict
	if noProgressEnabled && resultOK {
		noProgress = g.progress.Observe(result)
	}

	// 1. 工具调用总数判断
	if g.curToolTotalCallings >= maxToolTotalCallings {
		return newLoopGuardVerdict(LoopRuleTotalCallings, maxToolTotalCallingsStr, maxToolTotalCallings)
	}

	// 2. 连续失败
	if g.curConsecutiveCallErrors >= maxConsecutiveCallErrors {
		return newLoopGuardVerdict(LoopRuleConsecutiveErrors, maxConsecutiveCallErrorsStr, maxConsecutiveCallErrors)
	}

	// 3. 同一工具连续调用
	if g.curSameToolCallings >= maxSameToolCallings {
		return newLoopGuardVerdict(LoopRuleSameToolCallings, maxSameToolCallingsStr, maxSameToolCallings, toolName)
	}

	// 4. 完全相同的调用
	if len(g.recentTools) >= maxIdenticalToolCallings {
		tail := g.recentTools[len(g.recentTools)-maxIdenticalToolCallings:] //取最近的几个

		if pCommon.AllSame(tail) {
			return newLoopGuardVerdict(LoopRuleIdenticalCallings, maxIdenticalToolCallingMsg, maxIdenticalToolCallings, toolName)
		}
	}

	// 5. 二元交替检测(A,B,A,B...)
	if len(g.recentTools) >= maxAlternatingToolCallings*2 {
		tail := g.recentTools[len(g.recentTools)-maxAlternatingToolCallings*2:] //取最近的几轮

		if pCommon.IsAlternative(tail) {
			if strings.EqualFold(prevToolName, toolName) { //同一工具、两组参数交替
				return newLoopGuardVerdict(LoopRuleAlternatingCallings, maxAlternatingSameToolCallingMsg, maxAlternatingToolCallings, toolName)
			}

			return newLoopGuardVerdict(LoopRuleAlternatingCallings, maxAlternatingToolCallingMsg, maxAlternatingToolCallings, prevToolName, toolName)
		}
	}

	// 6. 无新进展检测
	if noProgress.Stalled {
		return newLoopGuardVerdict(LoopRuleNoProgress, maxNoProgressMsg, noProgress.ConsecutiveNoNew)
	}

	return LoopGuardVerdict{}
}
