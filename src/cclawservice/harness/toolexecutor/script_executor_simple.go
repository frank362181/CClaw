package toolexecutor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	pCommon "my.favorite/platformutility/common"
)

type SimpleScriptExecutor struct{}

var simpleScriptExecutor = pCommon.GetSingleton(func() *SimpleScriptExecutor {
	return &SimpleScriptExecutor{}
}).Instance()

func GetSimpleScriptExecutor() IScriptRunner {
	return simpleScriptExecutor
}

func (e *SimpleScriptExecutor) Run(ctx context.Context, arg ScriptArgument) (string, error) {
	if arg.OnLine == nil {
		return runScriptWithoutOnLine(ctx, arg.Path, arg.Args, arg.WorkRoot, arg.Sandbox)
	}

	return runScriptWithOnLine(ctx, arg.Path, arg.Args, arg.WorkRoot, arg.Sandbox, arg.OnLine)
}

// ============================ 脚本执行的参数、环境和计划=============

// limitedBuffer 限量捕获子进程输出,防止脚本输出洪泛。
type limitedBuffer struct {
	buf      bytes.Buffer
	limit    int
	exceeded bool
}

func (w *limitedBuffer) Write(p []byte) (int, error) {
	if w.buf.Len() >= w.limit {
		w.exceeded = true
		return len(p), nil
	}
	remaining := w.limit - w.buf.Len()
	if len(p) > remaining {
		w.buf.Write(p[:remaining])
		w.exceeded = true
		return len(p), nil
	}
	w.buf.Write(p)
	return len(p), nil
}

func (w *limitedBuffer) String() string { return w.buf.String() }

// ScriptPolicy 脚本执行策略(安全护栏参数)。所有脚本工具复用同一套默认值,
// 需要特殊护栏的调用方可自行定制(如更短超时/更高输出上限)。
type ScriptPolicy struct {
	ScriptBin        string        // 解释器,如 python3
	Timeout          time.Duration // 单次执行超时
	MaxOutputSize    int           // stdout/stderr 各自上限(字节)
	MaxInputJSONSize int           // 传入参数 JSON 上限(字节)
}

// DefaultScriptPolicy 默认安全护栏:python3 / 120s(可用 CCLAW_SCRIPT_TIMEOUT_SEC 覆盖) / 256KB / 64KB。
var DefaultScriptPolicy = ScriptPolicy{
	ScriptBin:        scriptBin,
	Timeout:          resolveScriptTimeout(),
	MaxOutputSize:    scriptMaxOutput,
	MaxInputJSONSize: scriptMaxInputJSON,
}

func runScriptWithoutOnLine(ctx context.Context, scriptPath, argsJSON string, workspaceRoot string, sandbox SandBoxSpec) (string, error) {
	scriptPolicy := normalizeScriptPolicy()

	info, err := os.Stat(scriptPath)
	if err != nil || info.IsDir() {
		return "", fmt.Errorf("script tool: script not found: %s", scriptPath)
	}
	if len(argsJSON) > scriptPolicy.MaxInputJSONSize {
		return "", fmt.Errorf("script tool: args too large (%d bytes)", len(argsJSON))
	}

	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, scriptPolicy.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, scriptPolicy.ScriptBin, scriptPath)
	cmd.Stdin = strings.NewReader(argsJSON)
	cmd.Env = getProcessEnv(workspaceRoot, sandbox.Enabled)
	if workspaceRoot != "" {
		cmd.Dir = workspaceRoot
	}

	stdoutBuf := limitedBuffer{limit: scriptPolicy.MaxOutputSize}
	stderrBuf := limitedBuffer{limit: scriptPolicy.MaxOutputSize}
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	// 注入沙箱(必须在 stdio设置之后、run之前)
	cmd, cleanup, err := prepareSandboxedCommand(ctx, &sandbox, cmd)
	if err != nil {
		return pCommon.BlankString, err
	}
	defer cleanup()

	runErr := cmd.Run()
	if stdoutBuf.exceeded || stderrBuf.exceeded {
		return "", fmt.Errorf("script tool: %s output exceeds %d bytes", filepath.Base(scriptPath), scriptPolicy.MaxOutputSize)
	}
	if runErr != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("script tool: %s timed out after %s", filepath.Base(scriptPath), scriptPolicy.Timeout)
		}
		detail := scriptFailureDetail(stderrBuf.String(), stdoutBuf.String(), runErr)
		return "", fmt.Errorf("script tool: %s failed: %s", filepath.Base(scriptPath), detail)
	}
	return strings.TrimSpace(stdoutBuf.String()), nil
}

func runScriptWithOnLine(ctx context.Context, scriptPath, argsJSON string, workspaceRoot string, sandbox SandBoxSpec, onLine func(string)) (string, error) {
	policy := normalizeScriptPolicy()

	info, err := os.Stat(scriptPath)
	if err != nil || info.IsDir() {
		return "", fmt.Errorf("script tool: script not found: %s", scriptPath)
	}
	if len(argsJSON) > policy.MaxInputJSONSize {
		return "", fmt.Errorf("script tool: args too large (%d bytes)", len(argsJSON))
	}

	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, policy.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, policy.ScriptBin, scriptPath)
	cmd.Stdin = strings.NewReader(argsJSON)
	cmd.Env = getProcessEnv(workspaceRoot, sandbox.Enabled)
	if workspaceRoot != "" {
		cmd.Dir = workspaceRoot
	}

	stderrBuf := limitedBuffer{limit: policy.MaxOutputSize}
	cmd.Stderr = &stderrBuf

	// 沙箱注入:必须在StdoutPipe之前
	// 否则管道建在原始 cmd上,而实际 Start 的是包装后的cmd,管道永远拿不到数据
	cmd, cleanup, err := prepareSandboxedCommand(ctx, &sandbox, cmd)
	if err != nil {
		return pCommon.BlankString, err
	}
	defer cleanup()

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("script tool: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("script tool: start failed: %w", err)
	}

	var sb strings.Builder
	exceeded := false
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 64*1024), policy.MaxOutputSize) // 单行上限 = 输出上限
	for scanner.Scan() {
		line := scanner.Text()
		if sb.Len()+len(line)+1 > policy.MaxOutputSize {
			exceeded = true
			_ = cmd.Process.Kill()
			break
		}
		sb.WriteString(line)
		sb.WriteString("\n")
		if onLine != nil && strings.TrimSpace(line) != "" {
			onLine(line)
		}
	}
	waitErr := cmd.Wait()

	if exceeded || scanner.Err() != nil {
		return "", fmt.Errorf("script tool: %s output exceeds %d bytes", filepath.Base(scriptPath), policy.MaxOutputSize)
	}
	if waitErr != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("script tool: %s timed out after %s", filepath.Base(scriptPath), policy.Timeout)
		}
		detail := scriptFailureDetail(stderrBuf.String(), sb.String(), waitErr)
		return "", fmt.Errorf("script tool: %s failed: %s", filepath.Base(scriptPath), detail)
	}
	return strings.TrimSpace(sb.String()), nil
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
