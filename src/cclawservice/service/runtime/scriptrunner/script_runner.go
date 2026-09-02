/**
* 脚本工具执行器(ScriptRunner)
*
* 职责:执行 SKILL.md 声明的脚本工具(scripts/*.py 等)。
*   1. 参数 JSON 通过 stdin 传入(避免 argv 长度/转义/注入问题);
*   2. stdout 为执行结果(约定 JSON 或文本),stderr 为错误信息;
*   3. 退出码非 0 或输出超限视为失败,由上层以 tool 消息回填给 LLM。
*
* 安全:仅执行 ToolSet 解析并校验过的脚本绝对路径(位于 agent extension 根内);
* 不使用 shell;内置超时与输出上限,防止脚本挂起/洪泛打爆内存。
**/

package scriptrunner

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	scriptBin          = "python3"        // 脚本解释器(与 SKILL.md requires.bins 对齐)
	scriptRunTimeout   = 15 * time.Second // 单次脚本执行超时
	scriptMaxOutput    = 256 * 1024       // stdout/stderr 各 256KB 上限
	scriptMaxInputJSON = 64 * 1024        // 传入参数 JSON 上限
)

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
	Bin          string        // 解释器,如 python3
	Timeout      time.Duration // 单次执行超时
	MaxOutput    int           // stdout/stderr 各自上限(字节)
	MaxInputJSON int           // 传入参数 JSON 上限(字节)
}

// DefaultScriptPolicy 默认安全护栏:python3 / 15s / 256KB / 64KB。
var DefaultScriptPolicy = ScriptPolicy{
	Bin:          scriptBin,
	Timeout:      scriptRunTimeout,
	MaxOutput:    scriptMaxOutput,
	MaxInputJSON: scriptMaxInputJSON,
}

// RunScript 以默认策略执行外部脚本工具(向后兼容入口)。
func RunScript(ctx context.Context, scriptPath, argsJSON string) (string, error) {
	return runScriptWithPolicy(ctx, DefaultScriptPolicy, scriptPath, argsJSON)
}

// RunScriptStream 流式执行脚本(stdout 逐行回调 onLine,去行尾换行、空行跳过),
// 返回完整输出(去尾部空白)与错误(退出码非 0/超限/超时)。
// 适用于需要把工具执行过程实时回传的调用方(如全流式工具进度)。
// 安全护栏与 runScriptWithPolicy 一致;超限即终止进程并报错;ctx 取消即终止。
func RunScriptStream(ctx context.Context, scriptPath, argsJSON string, onLine func(string)) (string, error) {
	policy := DefaultScriptPolicy
	if policy.Bin == "" {
		policy.Bin = scriptBin
	}
	if policy.Timeout <= 0 {
		policy.Timeout = scriptRunTimeout
	}
	if policy.MaxOutput <= 0 {
		policy.MaxOutput = scriptMaxOutput
	}
	if policy.MaxInputJSON <= 0 {
		policy.MaxInputJSON = scriptMaxInputJSON
	}

	info, err := os.Stat(scriptPath)
	if err != nil || info.IsDir() {
		return "", fmt.Errorf("script tool: script not found: %s", scriptPath)
	}
	if len(argsJSON) > policy.MaxInputJSON {
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

	cmd := exec.CommandContext(ctx, policy.Bin, scriptPath)
	cmd.Stdin = strings.NewReader(argsJSON)

	stderrBuf := limitedBuffer{limit: policy.MaxOutput}
	cmd.Stderr = &stderrBuf

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
	scanner.Buffer(make([]byte, 0, 64*1024), policy.MaxOutput) // 单行上限 = 输出上限
	for scanner.Scan() {
		line := scanner.Text()
		if sb.Len()+len(line)+1 > policy.MaxOutput {
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
		return "", fmt.Errorf("script tool: %s output exceeds %d bytes", filepath.Base(scriptPath), policy.MaxOutput)
	}
	if waitErr != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("script tool: %s timed out after %s", filepath.Base(scriptPath), policy.Timeout)
		}
		detail := strings.TrimSpace(stderrBuf.String())
		if detail == "" {
			detail = waitErr.Error()
		}
		return "", fmt.Errorf("script tool: %s failed: %s", filepath.Base(scriptPath), detail)
	}
	return strings.TrimSpace(sb.String()), nil
}

// runScriptWithPolicy 按指定策略执行外部脚本工具:
//   - argsJSON:参数 JSON(写到 stdin);
//   - 返回 stdout 文本(去尾部空白)。
//
// 退出码非 0、输出超限或参数超限时返回错误(含 stderr 摘要)。
// 未设置的策略字段回退到默认值,保证零值可用。
func runScriptWithPolicy(ctx context.Context, policy ScriptPolicy, scriptPath, argsJSON string) (string, error) {
	if policy.Bin == "" {
		policy.Bin = scriptBin
	}
	if policy.Timeout <= 0 {
		policy.Timeout = scriptRunTimeout
	}
	if policy.MaxOutput <= 0 {
		policy.MaxOutput = scriptMaxOutput
	}
	if policy.MaxInputJSON <= 0 {
		policy.MaxInputJSON = scriptMaxInputJSON
	}

	info, err := os.Stat(scriptPath)
	if err != nil || info.IsDir() {
		return "", fmt.Errorf("script tool: script not found: %s", scriptPath)
	}
	if len(argsJSON) > policy.MaxInputJSON {
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

	cmd := exec.CommandContext(ctx, policy.Bin, scriptPath)
	cmd.Stdin = strings.NewReader(argsJSON)

	stdoutBuf := limitedBuffer{limit: policy.MaxOutput}
	stderrBuf := limitedBuffer{limit: policy.MaxOutput}
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()
	if stdoutBuf.exceeded || stderrBuf.exceeded {
		return "", fmt.Errorf("script tool: %s output exceeds %d bytes", filepath.Base(scriptPath), policy.MaxOutput)
	}
	if runErr != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("script tool: %s timed out after %s", filepath.Base(scriptPath), policy.Timeout)
		}
		detail := strings.TrimSpace(stderrBuf.String())
		if detail == "" {
			detail = runErr.Error()
		}
		return "", fmt.Errorf("script tool: %s failed: %s", filepath.Base(scriptPath), detail)
	}
	return strings.TrimSpace(stdoutBuf.String()), nil
}
