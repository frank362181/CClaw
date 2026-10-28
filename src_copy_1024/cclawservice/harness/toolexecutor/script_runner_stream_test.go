/**
* ScriptRunner 流式执行单元测试(本地临时脚本,无网络依赖)
**/

package toolexecutor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunScriptStream_OnLine 验证 stdout 逐行回调与完整输出返回。
func TestRunScriptStream_OnLine(t *testing.T) {
	extRoot := t.TempDir()
	scriptPath := filepath.Join(extRoot, "multi.py")
	py := "import sys\nprint('line1')\nprint('line2')\nprint('line3')\n"
	if err := os.WriteFile(scriptPath, []byte(py), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	var lines []string
	out, err := RunScriptStream(context.Background(), scriptPath, "{}", "", func(line string) {
		lines = append(lines, line)
	})
	if err != nil {
		t.Fatalf("run stream: %v", err)
	}
	if len(lines) != 3 || lines[0] != "line1" || lines[1] != "line2" || lines[2] != "line3" {
		t.Errorf("onLine lines = %v, want [line1 line2 line3]", lines)
	}
	if !strings.Contains(out, "line2") {
		t.Errorf("full output missing line2: %q", out)
	}
}

// TestRunScriptStream_OnLineNil 验证 onLine 为空时仍返回完整输出。
func TestRunScriptStream_OnLineNil(t *testing.T) {
	extRoot := t.TempDir()
	scriptPath := filepath.Join(extRoot, "one.py")
	if err := os.WriteFile(scriptPath, []byte("print('hello')\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	out, err := RunScriptStream(context.Background(), scriptPath, "{}", "", nil)
	if err != nil {
		t.Fatalf("run stream: %v", err)
	}
	if strings.TrimSpace(out) != "hello" {
		t.Errorf("output = %q, want hello", out)
	}
}
