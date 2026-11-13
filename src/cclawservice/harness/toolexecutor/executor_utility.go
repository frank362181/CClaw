package toolexecutor

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	hCommon "my.favorite/cclawservice/harness/common"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/concurrency"
	"my.favorite/platformutility/logging"
)

// 归一化 工具调用收敛分层
func normalizeToolRoundTiming(t ToolRoundTiming) ToolRoundTiming {
	scriptTimeout := resolveScriptTimeout()

	if t.RoundTimeout <= 0 {
		t.RoundTimeout = time.Duration(pCommon.GetEnvIntDefault(EnvToolRoundTimeoutName, DefaultToolRoundTimeout)) * time.Second
	}

	if t.RoundTimeout <= 0 {
		t.RoundTimeout = scriptTimeout // 跟随脚本执行的超时设置:执行器绝不能比工具先放弃
	}

	if t.WaitTimeout <= 0 {
		t.WaitTimeout = time.Duration(pCommon.GetEnvIntDefault(EnvToolRoundWaitTimeoutName, DefaultToolWaitTimeout)) * time.Second
	}

	// 1. 工具调用正常收敛不得短于脚本执行的超时时间
	if t.RoundTimeout < scriptTimeout {
		logging.LogWarnF("tool executor: round timeout %s < script timeout %s, raise to script timeout",
			t.RoundTimeout, scriptTimeout)
		t.RoundTimeout = scriptTimeout
	}

	// 2.工具轮正常收敛超时不能超过回合超时
	turnTimeout := time.Duration(pCommon.GetEnvIntDefault(hCommon.ChatRunTimeout, hCommon.AgentRunTimeout)) * time.Second
	if turnTimeout > 0 && t.RoundTimeout > turnTimeout {
		logging.LogWarnF("tool executor: round timeout %s > turn timeout %s, clamp to turn timeout",
			t.RoundTimeout, turnTimeout)
		t.RoundTimeout = turnTimeout
	}
	return t
}

// resolveScriptTimeout 返回脚本执行超时:优先取环境变量 CCLAW_SCRIPT_TIMEOUT_SEC(秒,>=1),
// 未设置/非法时回退默认 scriptRunTimeout。用于给 OCR 等长耗时脚本扩容,避免 15s 硬超时。
func resolveScriptTimeout() time.Duration {
	if v := pCommon.GetEnvString("CCLAW_SCRIPT_TIMEOUT_SEC"); v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
			return time.Duration(sec) * time.Second
		}
	}
	return scriptRunTimeout
}

func normalizeScriptPolicy() ScriptPolicy {
	scriptPolicy := DefaultScriptPolicy
	if scriptPolicy.ScriptBin == "" {
		scriptPolicy.ScriptBin = scriptBin
	}
	if scriptPolicy.Timeout <= 0 {
		scriptPolicy.Timeout = scriptRunTimeout
	}
	if scriptPolicy.MaxOutputSize <= 0 {
		scriptPolicy.MaxOutputSize = scriptMaxOutput
	}
	if scriptPolicy.MaxInputJSONSize <= 0 {
		scriptPolicy.MaxInputJSONSize = scriptMaxInputJSON
	}

	return scriptPolicy
}

// scriptFailureDetail 提取脚本失败的可读原因。
// 优先级:stderr 详情 → stdout 的 {"error":"..."} → 执行错误(如 "exit status 1")。
// 为什么要回退解析 stdout:部分脚本(如 weather.py)把错误 JSON 只写到 stdout,
// 若仅看 stderr 会退化成 "exit status 1",真实原因(网络/解析/参数)全部丢失、无法诊断。
func scriptFailureDetail(stderr, stdout string, runErr error) string {
	if detail := strings.TrimSpace(stderr); detail != "" {
		return detail
	}
	if detail := extractStdoutError(stdout); detail != "" {
		return detail
	}
	return runErr.Error()
}

// extractStdoutError 从脚本 stdout 中提取 {"error":"..."} 文本。
// stdout 可能多行(最后一行通常是脚本的最终 JSON 结果),故自后向前找首个可解析且含 error 的 JSON 对象。
func extractStdoutError(stdout string) string {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if len(line) < 2 || line[0] != '{' {
			continue
		}

		var payload struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			continue
		}
		if msg := strings.TrimSpace(payload.Error); msg != "" {
			return msg
		}
	}
	return ""
}

// 等待一个scope收敛
func waitScope(ctx context.Context, scope concurrency.IRoutineScope, timing ToolRoundTiming) error {
	if ctx.Err() != nil {
		graceCtx, graceCancel := context.WithTimeout(context.Background(), timing.WaitTimeout)
		defer graceCancel()

		return scope.Wait(graceCtx)
	}

	roundCtx, roundCancel := context.WithTimeout(context.Background(), timing.RoundTimeout)
	defer roundCancel()

	// ctx 取消 -> 提前结束"等满轮上限"(否则就要白等工具执行的时间了)
	stopAfter := context.AfterFunc(ctx, roundCancel)
	defer stopAfter()

	waitErr := scope.Wait(roundCtx)
	if waitErr == nil || ctx.Err() == nil {
		return waitErr // 已经收敛
	}

	graceCtx, graceCancel := context.WithTimeout(context.Background(), timing.WaitTimeout)
	defer graceCancel()
	if graceErr := scope.Wait(graceCtx); graceErr != nil {
		return fmt.Errorf("caller canceled: drain timeout after %s", timing.WaitTimeout)
	}

	return nil
}

func normalizeOutcomes(invocations []ToolInvocation, dispatched []bool, out []ToolOutcome, outMu *sync.Mutex,
	roundStart time.Time) []ToolOutcome {
	elapse := time.Since(roundStart).Milliseconds()
	res := make([]ToolOutcome, 0, len(invocations))

	for i := range invocations {
		if !dispatched[i] {
			continue
		}

		outMu.Lock()
		oc := out[i]
		outMu.Unlock()

		if oc.CallID == pCommon.BlankString {
			inv := invocations[i]
			oc = ToolOutcome{
				Seq:      inv.Seq,
				CallID:   inv.ID(),
				Name:     inv.Name(),
				Arg:      inv.Args(),
				Reason:   fmt.Errorf("tool round timeout:tool %s(seq=%d) unfinished after %d ms", inv.Name(), inv.Seq, elapse),
				ElapseMs: elapse,
				RespAt:   pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp,
			}

			logging.LogWarnF("tool executor: tool %s(seq=%d,call=%s) unfinished after %dms, report as timeout outcome",
				inv.Name(), inv.Seq, inv.ID(), elapse)
		}

		res = append(res, oc)
	}

	return res
}
