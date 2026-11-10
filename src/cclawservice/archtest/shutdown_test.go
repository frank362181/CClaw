package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// shutdownCallAllowlist 允许调用"进程级终态操作"的位置。
//
// [为什么需要这条守卫]
// TaskManager 是进程级单例(sync.Once 构造,无 reset/替换入口),而下列操作都是一次性终态:
//   - Drain()        → tm.draining 永久置 true(全仓只有一处写入,且不可逆。这是 Drain
//     正确性的前提:先关闸再 wg.Wait(),否则复位后新任务可在 Wait 之后 Add,导致漏等);
//   - CancelScope()  → scopeCtx 永久取消,NewScope 派生出的 ctx 一出生即 Done;
//   - CancelDaemon() → daemonCtx 永久取消,且 tm.daemons 去重表不释放(同名 daemon 再也注册不回来);
//   - Close(ctx)     → 以上三步的组合。
//
// 一旦在测试进程里被调用,同包后续所有用例的 Go/TryGo/Daemon/NewScope+Go 都会返回
// ErrTaskManagerClosed 并污染 Rejected 计数;更麻烦的是失败表现为"顺序相关的随机红"
// 或"静默通过的假阳性",属于最难定位的一类测试污染。
//
// [新增例外的门槛] 必须在这里留下"为什么只能在退出路径调用"的理由,否则就是在给终态操作开后门。
var shutdownCallAllowlist = []struct {
	Path string // 目录前缀(以 / 结尾)或具体文件路径,相对仓库根(src/)
	Why  string
}{
	{
		Path: "platformutility/concurrency/",
		Why:  "Manager 自身实现:Close 内部依次调用 CancelScope/CancelDaemon/Drain",
	},
	{
		Path: "cclawservice/service_main.go",
		Why:  "ServiceMain.Destroy 是进程退出的唯一出口,由 main() 的 defer 触发;此后进程退出,终态无害",
	},
}

// shutdownHit 一处命中。
type shutdownHit struct {
	File     string
	Line     int
	Method   string
	OnGlobal bool // 接收者是 GetTaskManager()(进程单例),而非本地实例
}

// shutdownManagerMethods 进程级终态方法名。
// 全仓除 concurrency 包外没有同名方法(已确认),因此按名字命中即可。
// Close 是通用名,额外要求接收者是 GetTaskManager() 的结果(直接调用或经变量别名)。
const shutdownManagerMethodClose = "Close"

// matchAllowlistPath 判断 rel 是否命中某条白名单模式。
func matchAllowlistPath(rel, pattern string) bool {
	rel = filepath.ToSlash(rel)

	if strings.HasSuffix(pattern, "/") {
		return strings.HasPrefix(rel, pattern)
	}

	return rel == pattern
}

// allowedByShutdownAllowlist 判断某个相对路径是否允许调用终态操作。
func allowedByShutdownAllowlist(rel string) (bool, string) {
	for _, item := range shutdownCallAllowlist {
		if matchAllowlistPath(rel, item.Path) {
			return true, item.Why
		}
	}

	return false, ""
}

// managerAliases 收集"从 GetTaskManager() 取得的变量名",形如:
//
//	m := concurrency.GetTaskManager()
//	var m = concurrency.GetTaskManager()
//
// 否则 m.Close(ctx) 这种经过一次赋值的写法会漏检。
func managerAliases(file *ast.File) map[string]bool {
	aliases := make(map[string]bool)

	isGetTaskManagerCall := func(e ast.Expr) bool {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return false
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)

		return ok && sel.Sel.Name == "GetTaskManager"
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			for i, rhs := range node.Rhs {
				if i >= len(node.Lhs) || !isGetTaskManagerCall(rhs) {
					continue
				}

				if id, ok := node.Lhs[i].(*ast.Ident); ok {
					aliases[id.Name] = true
				}
			}
		case *ast.ValueSpec:
			for i, val := range node.Values {
				if i >= len(node.Names) || !isGetTaskManagerCall(val) {
					continue
				}

				aliases[node.Names[i].Name] = true
			}
		}

		return true
	})

	return aliases
}

// isManagerExpr 判断表达式是否"指向 TaskManager 实例"(直接调用 GetTaskManager() 或别名变量)。
func isManagerExpr(e ast.Expr, aliases map[string]bool) bool {
	found := false

	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}

		switch node := n.(type) {
		case *ast.CallExpr:
			if sel, ok := node.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "GetTaskManager" {
				found = true
			}
		case *ast.Ident:
			if aliases[node.Name] {
				found = true
			}
		}

		return true
	})

	return found
}

// findShutdownCalls 在单个文件里找出所有终态操作调用点。
//
// 已知局限:若接收者来自函数参数(如 func destroy(m IRoutineManager)),无法静态判定其类型。
// 当前生产代码不存在这种写法;新增时请直接调用 GetTaskManager() 或就地取别名。
func findShutdownCalls(file *ast.File, fSet *token.FileSet) []shutdownHit {
	aliases := managerAliases(file)

	var hits []shutdownHit

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		switch sel.Sel.Name {
		case "Drain", "CancelScope", "CancelDaemon":
			hits = append(hits, shutdownHit{
				Line:     fSet.Position(call.Pos()).Line,
				Method:   sel.Sel.Name,
				OnGlobal: isManagerExpr(sel.X, aliases),
			})
		case shutdownManagerMethodClose:
			if isManagerExpr(sel.X, aliases) {
				hits = append(hits, shutdownHit{
					Line:     fSet.Position(call.Pos()).Line,
					Method:   sel.Sel.Name,
					OnGlobal: true,
				})
			}
		}

		return true
	})

	return hits
}

// scanShutdownCalls 遍历仓库,返回所有命中(带相对路径)与解析错误。
func scanShutdownCalls(root string) ([]shutdownHit, []string) {
	var (
		hits      []shutdownHit
		parseErrs []string
	)

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // 单个条目不可读不应让整条守卫失效
		}

		if d.IsDir() {
			if skipArchtestDir(d.Name()) {
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil //nolint:nilerr
		}

		fSet := token.NewFileSet()

		file, parseErr := parser.ParseFile(fSet, path, nil, 0)
		if parseErr != nil {
			parseErrs = append(parseErrs, filepath.ToSlash(rel)+": "+parseErr.Error())

			return nil
		}

		for _, hit := range findShutdownCalls(file, fSet) {
			hit.File = filepath.ToSlash(rel)
			hits = append(hits, hit)
		}

		return nil
	})

	return hits, parseErrs
}

// TestShutdownCallsOnlyAtExit 守护:终态操作只能出现在进程退出路径,测试文件一律禁止。
//
// 这条守卫把"永不调用 Drain/CancelDaemon"从注释里的口头约定变成 CI 强制项 ——
// 任何"某个新测试为了验证 drain 顺手调一下"都会被立刻拦截。
func TestShutdownCallsOnlyAtExit(t *testing.T) {
	root := repoRoot(t)

	hits, parseErrs := scanShutdownCalls(root)
	if len(parseErrs) > 0 {
		t.Fatalf("archtest: 有文件无法解析,守卫会失效,请先修复: %v", parseErrs)
	}

	var violations []shutdownHit

	for _, hit := range hits {
		if strings.HasSuffix(hit.File, "_test.go") {
			// 测试里唯一的标准是"接收者是不是全局单例":
			//   - 作用于本地实例(NewTaskManager):安全,允许(用完即弃);
			//   - 作用于 GetTaskManager():会毒化同进程后续所有用例,禁止。
			if hit.OnGlobal {
				violations = append(violations, hit)
			}

			continue
		}

		if ok, _ := allowedByShutdownAllowlist(hit.File); ok {
			continue
		}

		violations = append(violations, hit)
	}

	if len(violations) == 0 {
		return
	}

	sort.Slice(violations, func(i, j int) bool {
		if violations[i].File != violations[j].File {
			return violations[i].File < violations[j].File
		}

		return violations[i].Line < violations[j].Line
	})

	for _, v := range violations {
		where := "生产代码"
		hint := "只能在进程退出路径调用;验证关停行为请在独立实例上调用(见 concurrency.NewTaskManager)"

		if strings.HasSuffix(v.File, "_test.go") {
			where = "测试文件(禁止作用于全局单例)"
			hint = "请改用 concurrency.NewTaskManager(cfg) 创建独立实例(可安全调用这些方法)," +
				"并用 concurrency.SwapTaskManager(m) 注入与还原"
		} else if v.OnGlobal {
			hint = "该调用作用在进程单例上;生产代码中只允许出现在进程退出路径"
		}

		t.Errorf("%s:%d [%s] 调用了 %s —— 这是进程级终态操作:"+
			"draining 永久置位、scopeCtx/daemonCtx 一经取消不可复活,"+
			"且单例无 reset 入口,调用后同进程后续所有用例都会被 ErrTaskManagerClosed 拒绝。"+
			"%s;若确属退出路径的新出口,请在 archtest.shutdownCallAllowlist 里注明理由",
			v.File, v.Line, where, v.Method, hint)
	}

	t.Fatalf("共 %d 处终态操作越界调用", len(violations))
}

// TestShutdownAllowlistEffective 守护白名单本身没写错。
//
// 两个方向都要查:
//  1. 条目必须真实存在且写了理由(否则是路径写错/代码已迁移);
//  2. 条目必须**真的命中**至少一处调用 —— 否则说明这个退出出口已经不存在
//     (例如 Destroy 里删掉了 Close(nil)),而守卫会静默失效。
func TestShutdownAllowlistEffective(t *testing.T) {
	root := repoRoot(t)

	hits, parseErrs := scanShutdownCalls(root)
	if len(parseErrs) > 0 {
		t.Fatalf("archtest: 有文件无法解析: %v", parseErrs)
	}

	for _, item := range shutdownCallAllowlist {
		abs := filepath.Join(root, filepath.FromSlash(item.Path))

		if _, err := os.Stat(abs); err != nil {
			t.Errorf("白名单条目不存在(路径写错或代码已迁移,请删除该条目): %s -> %v", item.Path, err)
		}

		if strings.TrimSpace(item.Why) == "" {
			t.Errorf("白名单条目缺少理由说明: %s", item.Path)
		}

		hit := 0

		for _, h := range hits {
			if matchAllowlistPath(h.File, item.Path) {
				hit++
			}
		}

		if hit == 0 {
			t.Errorf("白名单条目未命中任何终态调用: %s —— 说明该退出出口已不存在(或守卫已与代码脱节),请核实后删除该条目", item.Path)
		}
	}
}

// TestShutdownGuardMatcherSelfCheck 用内存源码自检匹配器,防止守卫因写错而"永远绿"。
func TestShutdownGuardMatcherSelfCheck(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "别名变量上调用 Drain",
			src:  "package p\nfunc f() { m := c.GetTaskManager(); m.Drain(nil) }",
			want: []string{"Drain@global"},
		},
		{
			name: "包级变量别名上调用 CancelDaemon",
			src:  "package p\nvar m = c.GetTaskManager()\nfunc f() { m.CancelDaemon() }",
			want: []string{"CancelDaemon@global"},
		},
		{
			name: "直接链式调用 Close",
			src:  "package p\nfunc f() { _ = c.GetTaskManager().Close(nil) }",
			want: []string{"Close@global"},
		},
		{
			name: "直接链式调用 CancelScope",
			src:  "package p\nfunc f() { c.GetTaskManager().CancelScope() }",
			want: []string{"CancelScope@global"},
		},
		{
			name: "本地实例上的 Close 不参与拦截(只拦全局单例)",
			src:  "package p\nfunc f() { m := concurrency.NewTaskManager(cfg); m.Close(nil) }",
			want: nil,
		},
		{
			name: "函数参数上的 Drain 不算全局",
			src:  "package p\nfunc f(m *concurrency.TaskManager) { m.Drain(ctx) }",
			want: []string{"Drain"},
		},
		{
			name: "普通资源的 Close 不误伤",
			src:  "package p\nfunc f(r io.ReadCloser) { r.Close() }",
			want: nil,
		},
		{
			name: "scope 的 Cancel 不误伤",
			src:  "package p\nfunc f(s scope) { s.Cancel() }",
			want: nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fSet := token.NewFileSet()

			file, err := parser.ParseFile(fSet, "snippet.go", c.src, 0)
			if err != nil {
				t.Fatalf("解析自检源码失败: %v", err)
			}

			var got []string
			for _, hit := range findShutdownCalls(file, fSet) {
				s := hit.Method
				if hit.OnGlobal {
					s += "@global"
				}

				got = append(got, s)
			}

			sort.Strings(got)

			want := append([]string(nil), c.want...)
			sort.Strings(want)

			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("匹配结果不符: got=%v want=%v", got, want)
			}
		})
	}
}
