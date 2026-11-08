// Package archtest 放置"架构约束"类测试:不进业务代码,只在 go test 时守护约定。
package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// goStatementAllowlist 允许出现裸 go 语句的位置。
//
// [为什么用 AST 测试而不是 golangci-lint]
// golangci-lint 没有"禁止 go 语句"的规则(forbidigo 只能匹配标识符/调用文本,
// 抓不到 go 关键字)。遍历 ast.GoStmt 是精确的,且零额外依赖,
// 随 `go test ./...` 在 CI 里天然生效。
//
// [新增例外的门槛] 必须在这里留下"为什么不能托管"的理由,否则就是在给裸 go 开后门。
var goStatementAllowlist = []struct {
	Path string // 目录前缀(以 / 结尾)或具体文件路径,相对仓库根(src/)
	Why  string
}{
	{
		Path: "platformutility/concurrency/",
		Why:  "Manager 自身:全仓唯一允许创建托管 goroutine 的地方(含 wg.Wait 的辅助协程)",
	},
	{
		Path: "tui/",
		Why:  "独立二进制(TUI 客户端),不参与服务端 Manager 的生命周期",
	},
	{
		Path: "platformutility/heaven/",
		Why:  "整包死代码(不可达),待删除;删除后请一并删掉这条白名单",
	},
	{
		Path: "cclawservice/harness/toolexecutor/function_calling_heartbeat.go",
		Why:  "工具心跳:生命周期已由 runOne 的 defer hb.Stop() 完整收敛,且已自带 recover;迁入 scope 会占用工具的扇出名额",
	},
	{
		Path: "platformutility/logging/loggingwriter/logging_writer.go",
		Why: "日志刷盘协程:**不能**托管 —— concurrency 为了打日志 import 了 logging," +
			"logging 又 import 了 loggingwriter,反向 import 会成循环依赖。" +
			"其生命周期已由 AsyncLogger.Close()(close(flushChannel) + waitGroup.Wait()) 收敛," +
			"由 ServiceMain.Destroy 末尾的 logging.CloseLogger() 触发",
	},
}

// repoRoot 从本测试文件位置反推仓库根(src/)。
// 文件路径形如 <root>/cclawservice/archtest/goroutine_test.go。
func repoRoot(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("archtest: cannot locate own source file")
	}

	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// allowedByAllowlist 判断某个相对路径是否在白名单内,并返回理由。
func allowedByAllowlist(rel string) (bool, string) {
	rel = filepath.ToSlash(rel)

	for _, item := range goStatementAllowlist {
		if strings.HasSuffix(item.Path, "/") {
			if strings.HasPrefix(rel, item.Path) {
				return true, item.Why
			}

			continue
		}

		if rel == item.Path {
			return true, item.Why
		}
	}

	return false, ""
}

// skipArchtestDir 跳过的目录(依赖/生成物/二进制部署目录)。
func skipArchtestDir(name string) bool {
	switch name {
	case ".git", "vendor", "node_modules", "bin":
		return true
	}

	return false
}

// TestNoBareGoStatement 守护:除白名单外,生产代码不得直接 `go xxx()`。
//
// 违规时的报错形如:
//
//	xxx.go:42 出现了裸 go 语句:改用 concurrency.GetTaskManager() 的 Go/TryGo/NewScope
func TestNoBareGoStatement(t *testing.T) {
	root := repoRoot(t)

	type violation struct {
		file string
		line int
	}

	var violations []violation

	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if skipArchtestDir(d.Name()) {
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil // 测试文件允许自由起 goroutine 做 SUT 编排
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		if ok, _ := allowedByAllowlist(rel); ok {
			return nil
		}

		fSet := token.NewFileSet()

		file, parseErr := parser.ParseFile(fSet, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}

		ast.Inspect(file, func(node ast.Node) bool {
			if _, isGo := node.(*ast.GoStmt); isGo {
				violations = append(violations, violation{
					file: filepath.ToSlash(rel),
					line: fSet.Position(node.Pos()).Line,
				})
			}

			return true
		})

		return nil
	})
	if walkErr != nil {
		t.Fatalf("archtest: walk repo failed: %v", walkErr)
	}

	if len(violations) == 0 {
		return
	}

	for _, v := range violations {
		t.Errorf("%s:%d 出现了裸 go 语句:改用 concurrency.GetTaskManager() 的 Go/TryGo/NewScope;"+
			"若确属例外,请在 archtest.goStatementAllowlist 里注明理由", v.file, v.line)
	}

	t.Fatalf("共 %d 处裸 go 语句未托管", len(violations))
}

// TestGoStatementAllowlistEffective 守护白名单本身没写错(写错了会静默失效)。
// 白名单里的每个条目都必须真实存在,否则说明路径写错或代码已迁移,该条目应删除。
func TestGoStatementAllowlistEffective(t *testing.T) {
	root := repoRoot(t)

	for _, item := range goStatementAllowlist {
		abs := filepath.Join(root, filepath.FromSlash(item.Path))

		if _, err := os.Stat(abs); err != nil {
			t.Errorf("白名单条目不存在(路径写错或代码已迁移,请删除该条目): %s -> %v", item.Path, err)
		}

		if strings.TrimSpace(item.Why) == "" {
			t.Errorf("白名单条目缺少理由说明: %s", item.Path)
		}
	}
}
