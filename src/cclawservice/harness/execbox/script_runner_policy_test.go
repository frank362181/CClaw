/**
* ScriptRunner 护栏策略单元测试(白盒,package execbox):
* 默认策略与安全常量一致;自定义策略超时生效。
**/

package execbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestScriptPolicy_Defaults 验证默认护栏策略非零且与安全常量一致。
// (ScriptPolicy 零值字段不回填——回退发生在 runScriptWithPolicy 内部,由 TestRunScriptWithPolicy_CustomTimeout 覆盖。)
func TestScriptPolicy_Defaults(t *testing.T) {
	p := DefaultScriptPolicy
	if p.Bin == "" || p.Timeout <= 0 || p.MaxOutput <= 0 || p.MaxInputJSON <= 0 {
		t.Fatalf("default policy must be non-zero, got %+v", p)
	}
	if p.Bin != scriptBin || p.Timeout != scriptRunTimeout || p.MaxOutput != scriptMaxOutput || p.MaxInputJSON != scriptMaxInputJSON {
		t.Errorf("default policy mismatch: %+v", p)
	}
}

// TestRunScriptWithPolicy_CustomTimeout 验证自定义策略超时生效。
func TestRunScriptWithPolicy_CustomTimeout(t *testing.T) {
	extRoot := t.TempDir()
	// 一个挂起(睡眠 5s)的脚本
	scriptPath := filepath.Join(extRoot, "hang.py")
	if err := os.WriteFile(scriptPath, []byte("import time\ntime.sleep(5)\nprint('done')\n"), 0o755); err != nil {
		t.Fatalf("write hang script: %v", err)
	}

	policy := DefaultScriptPolicy
	policy.Timeout = 100 * time.Millisecond
	start := time.Now()
	_, err := runScriptWithPolicy(context.Background(), policy, scriptPath, "{}")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected timeout error message, got: %v", err)
	}
	if elapsed > 3*time.Second {
		t.Errorf("timeout took too long: %v", elapsed)
	}
}
