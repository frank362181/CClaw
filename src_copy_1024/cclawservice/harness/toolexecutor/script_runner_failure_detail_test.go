/**
* ScriptRunner 失败原因提取单元测试(本地临时脚本,无网络依赖)
*
* 回归背景:部分脚本(如 weather.py)把错误 JSON 只写到 stdout、stderr 留空。
* 早期实现只读 stderr,详情退化成 "exit status 1",真实原因(网络/解析/参数)全部丢失、无法诊断。
* 现约定优先级:stderr 详情 → stdout 的 {"error":"..."} → 执行错误(exit status)。
**/

package toolexecutor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeScript 写一个临时 python 脚本并返回其路径。
func writeScript(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

// TestRunScript_FailureDetailFromStdout 回归:脚本只把错误写 stdout 时,
// 失败原因必须取自 stdout 的 {"error":"..."},而不是退化成 "exit status 1"。
func TestRunScript_FailureDetailFromStdout(t *testing.T) {
	// 模拟 weather.py:错误 JSON 只写 stdout,stderr 留空,退出码 1
	scriptPath := writeScript(t, "stdout_err.py",
		"import json,sys\nprint(json.dumps({'error': 'city not found: 北京X'}))\nsys.exit(1)\n")

	_, err := RunScript(context.Background(), scriptPath, "{}", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "city not found: 北京X") {
		t.Errorf("error detail should come from stdout, got: %v", err)
	}
	if strings.Contains(err.Error(), "exit status") {
		t.Errorf("should not degrade to exit status, got: %v", err)
	}
}

// TestRunScript_FailureDetailStderrPriority 验证 stderr 详情优先于 stdout。
func TestRunScript_FailureDetailStderrPriority(t *testing.T) {
	scriptPath := writeScript(t, "both_err.py",
		"import json,sys\nprint(json.dumps({'error': 'stdout-echo'}))\nsys.stderr.write('stderr-detail\\n')\nsys.exit(1)\n")

	_, err := RunScript(context.Background(), scriptPath, "{}", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "stderr-detail") {
		t.Errorf("stderr detail should win, got: %v", err)
	}
}

// TestRunScript_FailureDetailFallbackExitStatus 验证 stdout/stderr 都无详情时,
// 退回执行错误(exit status),保证错误信息始终非空。
func TestRunScript_FailureDetailFallbackExitStatus(t *testing.T) {
	scriptPath := writeScript(t, "silent_err.py", "import sys\nsys.exit(3)\n")

	_, err := RunScript(context.Background(), scriptPath, "{}", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "exit status 3") {
		t.Errorf("should fall back to exit status 3, got: %v", err)
	}
}

// TestRunScriptStream_FailureDetailFromStdout 验证流式路径同样回退解析 stdout。
func TestRunScriptStream_FailureDetailFromStdout(t *testing.T) {
	scriptPath := writeScript(t, "stream_err.py",
		"import json,sys\nprint(json.dumps({'error': 'stream-boom'}))\nsys.exit(1)\n")

	_, err := RunScriptStream(context.Background(), scriptPath, "{}", "", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "stream-boom") {
		t.Errorf("stream path detail should come from stdout, got: %v", err)
	}
}

// TestExtractStdoutError 覆盖 stdout 解析的边界情况。
func TestExtractStdoutError(t *testing.T) {
	cases := []struct {
		name   string
		stdout string
		want   string
	}{
		{"single line", `{"error": "boom"}`, "boom"},
		{"last line wins", "noise\n{\"error\": \"boom\"}\n", "boom"},
		{"no error field", `{"city": "北京"}`, ""},
		{"non json", "plain text", ""},
		{"empty", "   ", ""},
		{"empty error value", `{"error": "  "}`, ""},
		{"earlier braces without json ignored", "{\n{\"error\": \"late\"}", "late"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractStdoutError(c.stdout); got != c.want {
				t.Errorf("extractStdoutError(%q) = %q, want %q", c.stdout, got, c.want)
			}
		})
	}
}

// TestScriptFailureDetail_Priority 直接覆盖优先级:stderr → stdout → 执行错误。
func TestScriptFailureDetail_Priority(t *testing.T) {
	runErr := errors.New("exit status 1")

	if got := scriptFailureDetail("  stderr-detail ", `{"error":"stdout-detail"}`, runErr); got != "stderr-detail" {
		t.Errorf("stderr should win, got %q", got)
	}
	if got := scriptFailureDetail("", `{"error":"stdout-detail"}`, runErr); got != "stdout-detail" {
		t.Errorf("stdout should be used when stderr empty, got %q", got)
	}
	if got := scriptFailureDetail("", "", runErr); got != "exit status 1" {
		t.Errorf("should fall back to run error, got %q", got)
	}
}
