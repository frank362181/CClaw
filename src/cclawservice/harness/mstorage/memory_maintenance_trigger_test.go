/**
* 后台记忆维护(**编排**层)的触发契约
*
* 与 memory_maintenance_test.go 的分工:
*   - 那边锁的是 maintenancePlan.steps 的**顺序声明**;
*   - 这边锁的是"真跑起来之后发生了什么" —— 收尾路径把尾巴蒸出去、蒸馏确实发生在压缩之前。
*
* 为什么要补这些:编排搬到本包时一条用例都没有,于是
* "Assembled 由值参数变指针参数 → 收尾路径传 nil 被裸解引用"这个 panic 直接漏到了运行时
* (而且它跑在**调用方协程**上,后台协程的 recover 兜不住)。
**/

package mstorage

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	cCommon "my.favorite/cclawservice/common"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/ctxmgr"
	"my.favorite/cclawservice/model/dataaccessor"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/request"
)

// distilledFact 假的蒸馏产物(测试辅助):它出现在 MEMORY.md 里,即证明蒸馏真的落盘了。
const distilledFact = "报告统一使用中文,先给结论"

// closingQuestion 收尾用例的提问(测试辅助):含长期信号,否则蒸馏的本地预筛会跳过调用。
const closingQuestion = "以后所有报告统一用中文,先给结论;单位编码固定 100001。"

// newTriggerTestService 构造"启用 + 访问器就绪"的 MemoryService(测试辅助)。
// Enabled() 要求访问器就绪(收尾路径要回退查轮次数),所以必须搭一个临时库。
func newTriggerTestService(t *testing.T) *MemoryService {
	t.Helper()

	accessor, err := dataaccessor.GetSessionAccessor(newSessionTestDatabase(t))
	if err != nil {
		t.Fatalf("构建会话访问器失败: %v", err)
	}
	return &MemoryService{enabled: true, sessionAccessor: accessor}
}

// modelCall 一次后台维护对模型的调用(测试辅助)。
type modelCall struct {
	tag        string // 由系统提示归类出的动作名:distill / compact / merge
	userPrompt string
}

// maintenanceModelCalls 记录后台维护的模型调用序列。
// 摘要在**后台协程**里被调用,而断言在用例协程上 —— 必须加锁。
type maintenanceModelCalls struct {
	mu    sync.Mutex
	calls []modelCall
}

func newMaintenanceModelCalls() *maintenanceModelCalls {
	return &maintenanceModelCalls{}
}

func (c *maintenanceModelCalls) add(tag, userPrompt string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, modelCall{tag: tag, userPrompt: userPrompt})
}

func (c *maintenanceModelCalls) snapshot() []modelCall {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]modelCall, len(c.calls))
	copy(out, c.calls)
	return out
}

// tags 只取动作名,便于断言与报错输出。
func tags(calls []modelCall) []string {
	out := make([]string, 0, len(calls))
	for _, call := range calls {
		out = append(out, call.tag)
	}
	return out
}

// modelCallTag 把一次维护调用归类到具体动作:三种动作的系统提示互不相同。
func modelCallTag(systemPrompt string) string {
	switch {
	case systemPrompt == distillSystemPrompt:
		return "distill"
	case systemPrompt == compactionMergeSystemPrompt:
		return "merge"
	case systemPrompt == compactionSystemPrompt:
		return "compact"
	default:
		return "unknown"
	}
}

// maintenanceSummarizer 按动作返回各自的合法输出,并记录调用顺序(测试辅助)。
func maintenanceSummarizer(calls *maintenanceModelCalls) hCommon.Summarizer {
	return func(_ context.Context, systemPrompt, userPrompt string) (string, error) {
		tag := modelCallTag(systemPrompt)
		calls.add(tag, userPrompt)

		switch tag {
		case "distill":
			return "- " + distilledFact, nil
		case "compact":
			return strings.Repeat("历史摘要。", 10), nil
		case "merge":
			return strings.Repeat("合并摘要。", 10), nil
		default:
			return "", nil
		}
	}
}

// waitFor 轮询等待条件成立(后台维护是异步的,用例必须等它落地)。
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待超时:%s", what)
}

// TestTriggerMemoryMaintenanceClosingPathDistillsTail 收尾路径(Assembled=nil + Closing=true)必须跑通。
//
// 迁移后这条路径曾经**直接 panic**:签名的 AssembleResult 从"值"变"指针"后,
// EndChatSession 传的 nil 被裸解引用。而本函数跑在**调用方协程**上(后台协程里的 recover
// 兜不住它)—— 聊天/结束会话的主流程会跟着崩,违反"旁路增强不得影响主流程"。
//
// 这个用例因此同时锁住三件事:不在调用方 panic、nil 被归一、收尾确实把尾巴蒸出去(不是只走个流程)。
func TestTriggerMemoryMaintenanceClosingPathDistillsTail(t *testing.T) {
	svc := newTriggerTestService(t)
	agent := newDistillAgent(t, "maintenance_closing")

	// 3 轮问答 = 6 条记录,正好踩到**首次**蒸馏的记录门槛(DistillMinRecords)
	req := &request.ChatRequest{
		SessionId: "maint-close-1", TurnId: "turn-close-0", ReqMsg: closingQuestion, GenAt: 1770000000,
	}
	session, ok := svc.Begin("alice", agent, req)
	if !ok {
		t.Fatal("会话建档失败")
	}
	cleanupHistoryDir(t, session.SessionId)

	for i := 1; i <= 3; i++ {
		turn := &request.ChatRequest{
			SessionId: req.SessionId, TurnId: fmt.Sprintf("turn-close-%d", i),
			ReqMsg: closingQuestion, GenAt: 1770000000 + int64(i),
		}
		if seq := svc.AppendTurn(session, turn, "已记住。", nil); seq <= 0 {
			t.Fatalf("第 %d 轮落库失败", i)
		}
	}

	records, err := svc.ReadRecords(session)
	if err != nil {
		t.Fatalf("读回历史失败: %v", err)
	}
	if len(records) < hCommon.DistillMinRecords {
		t.Fatalf("用例前提不成立:应有至少 %d 条记录,实际 %d", hCommon.DistillMinRecords, len(records))
	}

	calls := newMaintenanceModelCalls()

	// 显式捕获 panic:收尾路径跑在调用方协程上,panic 会直接打死调用方
	// (测试进程因此崩溃,而 Assert 更清楚)。
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("收尾维护不得在调用方协程 panic: %v", r)
			}
		}()

		svc.TriggerMemoryMaintenance(MaintenanceRequest{
			Session:   &session,
			Agent:     &agent,
			Assembled: nil, // 收尾路径没有"本轮装配结果":这正是迁移后 panic 的输入
			Records:   records,
			Closing:   true,
		}, maintenanceSummarizer(calls))
	}()

	waitFor(t, "收尾蒸馏应发起模型调用(Closing 维度)", func() bool { return len(calls.snapshot()) > 0 })
	if got := calls.snapshot(); got[0].tag != "distill" {
		t.Fatalf("收尾路径应蒸馏,实际动作=%v", tags(got))
	}

	// 产物必须真的落进长期记忆:否则上一条断言只证明"调了一次模型"
	waitFor(t, "MEMORY.md 应写入收尾蒸馏结果", func() bool {
		return strings.Contains(readMemoryDocIfExists(t, agent), distilledFact)
	})
}

// TestTriggerMemoryMaintenanceDistillsBeforeCompacting 顺序是**行为**契约,不是注释。
//
// 同一轮里既蒸馏又压缩时,蒸馏必须看到**压缩之前的原文**。顺序反了的后果不可逆:
// 压缩会把整段记录变成一条摘要记录,而摘要记录不参与蒸馏 —— 那段原文对长期记忆永久不可见。
//
// 这里不看 steps() 的返回值,而是看真实调用序列与真实输入:
//  1. 第一次模型调用必须是蒸馏(而不是压缩);
//  2. 那次蒸馏的输入里必须含被压缩区间的原文(记录 1 起)。
func TestTriggerMemoryMaintenanceDistillsBeforeCompacting(t *testing.T) {
	svc := newTriggerTestService(t)
	agent := newDistillAgent(t, "maintenance_order")

	records := orderFixtureRecords() // 8 条普通记录:正文够长(过压缩收益门槛)+ 含长期信号
	const wantCovers = "covers: 1-8"

	sessionId := cCommon.NewCompactId("maintorder")
	// 独立的写入器(不污染全局单例的序号缓存):文件按路径定位,与读回路径一致
	fileName := writeRecords(t, &MemoryFileRWriter{nextIndex: make(map[string]int)}, sessionId, records)
	cleanupHistoryDir(t, sessionId)

	// 全局节流器按 sessionId 键控:用例清理干净,避免污染其它用例
	GetCompactionThrottle().Reset(sessionId)
	t.Cleanup(func() { GetCompactionThrottle().Reset(sessionId) })

	session := dataobject.TSession{SessionId: sessionId, ChatData: fileName}

	// 触发条件:历史被裁掉 1-8(压缩收益维度)+ 踩在首次蒸馏节拍上(蒸馏)
	assembled := ctxmgr.AssembleResult{
		Trimmed:            true,
		DroppedFrom:        1,
		DroppedTo:          len(records),
		DroppedRecordCount: len(records),
	}

	calls := newMaintenanceModelCalls()
	svc.TriggerMemoryMaintenance(MaintenanceRequest{
		Session:   &session,
		Agent:     &agent,
		Assembled: &assembled,
		Records:   records,
		TurnSeq:   hCommon.DistillMinTurns, // 用轮次序号踩节拍,用例不必依赖 DB 里的轮次
	}, maintenanceSummarizer(calls))

	waitFor(t, "压缩摘要应落盘", func() bool {
		return strings.Contains(readHistoryFile(t, sessionId, fileName), wantCovers)
	})

	got := calls.snapshot()
	if len(got) < 2 {
		t.Fatalf("本轮应同时蒸馏与压缩,实际模型调用=%v", tags(got))
	}
	if got[0].tag != "distill" || got[1].tag != "compact" {
		t.Fatalf("必须先蒸馏后压缩(顺序是契约),实际动作序列=%v", tags(got))
	}

	// 关键断言:蒸馏的**输入**含被压缩区间的原文。
	// 若顺序反了,压缩先跑,这段记录会变成摘要记录 —— ActivePlainRecords 会滤掉它,
	// 蒸馏拿到的就是别的(或什么都没有),这条断言会失败。
	if !strings.Contains(got[0].userPrompt, "[1] user:") {
		t.Fatalf("蒸馏输入应含记录 1 的原文(压缩区间起点),实际输入=\n%s", got[0].userPrompt)
	}
	if doc := readMemoryDocIfExists(t, agent); !strings.Contains(doc, distilledFact) {
		t.Errorf("被压缩区间的事实应已进入长期记忆,实际 MEMORY.md=\n%s", doc)
	}
}

// TestTriggerMemoryMaintenanceIgnoresIncompleteRequest 参数不完整必须"静默跳过",不得 panic。
// 这些解引用全在**调用方协程**上,漏一个就是主流程崩溃(与收尾用例同源的回归面)。
func TestTriggerMemoryMaintenanceIgnoresIncompleteRequest(t *testing.T) {
	svc := newTriggerTestService(t)
	agent := newDistillAgent(t, "maintenance_nil")
	session := dataobject.TSession{SessionId: "maint-nil-1", ChatData: "memory_20260915000000.md"}
	calls := newMaintenanceModelCalls()

	cases := []struct {
		name string
		req  MaintenanceRequest
	}{
		{"会话缺失", MaintenanceRequest{Agent: &agent}},
		{"Agent 缺失", MaintenanceRequest{Session: &session}},
		{"两者都缺", MaintenanceRequest{}},
		{"未建档(chat_data 为空)", MaintenanceRequest{Session: &dataobject.TSession{SessionId: "maint-nil-2"}, Agent: &agent}},
		// Assembled 缺省(zero value 指针为 nil)→ 归一为"无裁剪 + 预算未耗尽"后早退(无动作可做)
		{"装配结果缺失", MaintenanceRequest{Session: &session, Agent: &agent}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("参数不完整不得 panic: %v", r)
				}
			}()

			svc.TriggerMemoryMaintenance(c.req, maintenanceSummarizer(calls))
		})
	}

	if got := calls.snapshot(); len(got) != 0 {
		t.Errorf("参数不完整不应发起任何模型调用,实际=%v", tags(got))
	}
}

// orderFixtureRecords 8 条普通记录(4 轮问答,测试辅助)。
func orderFixtureRecords() []hCommon.MemoryRecord {
	return maintenanceRecords(8, true)
}

// TestDistillHonorsInputBudget 回归(F11):按窗口算出的源文本预算必须真的落到"选批"上 ——
// 否则小窗模型上维护调用会把请求顶出窗口,而失败只在后台日志里可见。
func TestDistillHonorsInputBudget(t *testing.T) {
	agent := newDistillAgent(t, "distill_budget")
	svc := &MemoryService{enabled: true}
	records := distillRecords() // 6 条

	lineRunes := func(i int) int { return utf8.RuneCountInString(compactionPromptLine(records[i])) }
	// 预算:恰好装得下前两条送审行,第 3 条放不下
	budget := lineRunes(0) + lineRunes(1) + 1
	if budget >= lineRunes(0)+lineRunes(1)+lineRunes(2) {
		t.Fatal("用例前提:该预算应装不下第 3 条")
	}

	bounded, err := svc.distillWithBudget(context.Background(), dataobject.TSession{SessionId: "budget-1"}, records, agent,
		fakeSummarizer("- 报告统一使用中文", nil), budget)
	if err != nil {
		t.Fatalf("蒸馏失败: %v", err)
	}
	if bounded.CoversFrom != records[0].Index || bounded.CoversTo != records[1].Index {
		t.Errorf("应只送前两条: covers=%d-%d", bounded.CoversFrom, bounded.CoversTo)
	}
	if bounded.Backlog != len(records)-2 {
		t.Errorf("积压应为 %d 条,实际 %d", len(records)-2, bounded.Backlog)
	}

	// 默认入口(硬上限)不受影响:6 条一次装得下
	full, err := svc.Distill(context.Background(), dataobject.TSession{SessionId: "budget-2"}, records, agent,
		fakeSummarizer("- 报告统一使用中文", nil))
	if err != nil {
		t.Fatalf("蒸馏失败: %v", err)
	}
	if full.Backlog != 0 || full.CoversTo != records[len(records)-1].Index {
		t.Errorf("硬上限下应一次装下全部: covers=%d-%d backlog=%d", full.CoversFrom, full.CoversTo, full.Backlog)
	}
}

// TestTriggerMemoryMaintenanceSkipsWhenWindowTooSmall 回归(F11):窗口连维护的输出额度都装不下时,
// 整轮维护直接跳过 —— 不发起任何模型调用(而不是发一个必然超窗的请求)。
func TestTriggerMemoryMaintenanceSkipsWhenWindowTooSmall(t *testing.T) {
	svc := newTriggerTestService(t)
	agent := newDistillAgent(t, "maintenance_window")

	records := maintenanceRecords(8, true)
	sessionId := cCommon.NewCompactId("maintwindow")
	fileName := writeRecords(t, &MemoryFileRWriter{nextIndex: make(map[string]int)}, sessionId, records)
	cleanupHistoryDir(t, sessionId)
	GetCompactionThrottle().Reset(sessionId)
	t.Cleanup(func() { GetCompactionThrottle().Reset(sessionId) })

	session := dataobject.TSession{SessionId: sessionId, ChatData: fileName}
	assembled := ctxmgr.AssembleResult{
		Trimmed: true, DroppedFrom: 1, DroppedTo: len(records), DroppedRecordCount: len(records),
	}

	calls := newMaintenanceModelCalls()
	svc.TriggerMemoryMaintenance(MaintenanceRequest{
		Session: &session, Agent: &agent, Assembled: &assembled, Records: records,
		TurnSeq:       hCommon.DistillMinTurns,  // 触发条件都成立:唯一被挡住的是窗口预算
		ContextWindow: hCommon.SummaryMaxTokens, // 窗口 == 维护输出额度 ⇒ 维护输入零空间
	}, maintenanceSummarizer(calls))

	if got := tags(calls.snapshot()); len(got) != 0 {
		t.Fatalf("窗口装不下时不应发起任何维护调用,实际 %v", got)
	}
	// 宽限期:确认后台**确实**没有动作。
	// 判定是同步的(未获槽、未起协程),但"什么都没有发生"这类否定断言必须等一会儿才作数 ——
	// 否则闸门缺失时协程还没来得及跑,用例会侥幸通过。
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		if len(calls.snapshot()) > 0 {
			t.Fatalf("窗口装不下时后台不应发起维护调用,实际 %v", tags(calls.snapshot()))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if history := readHistoryFile(t, sessionId, fileName); strings.Contains(history, "covers:") {
		t.Errorf("不应写入摘要记录:\n%s", history)
	}
	if doc := readMemoryDocIfExists(t, agent); doc != "" {
		t.Errorf("不应写入 MEMORY.md:\n%s", doc)
	}
}

// maintenanceRecords 构造 n 条普通记录(测试辅助,要求 n 为偶数:一轮 = 用户 + 助手):
//   - 正文足够长 → 压缩才有收益(源体积需大于摘要上限,否则被收益门槛跳过);
//   - withSignal=true 时用户记录含"以后/统一" → 蒸馏的本地预筛才放行
//     (否则不发起模型调用,断言会失真)。
func maintenanceRecords(n int, withSignal bool) []hCommon.MemoryRecord {
	records := make([]hCommon.MemoryRecord, 0, n)
	for i := 0; i < n/2; i++ {
		index := i*2 + 1
		question := fmt.Sprintf("第%d轮:继续推进这件事,细节%s", i+1, bulkBody())
		if withSignal {
			question = fmt.Sprintf("第%d轮:以后所有报告统一用中文,先给结论;细节%s", i+1, bulkBody())
		}
		records = append(records,
			record(index, hCommon.HistoryRecordTypeUser, "", question),
			record(index+1, hCommon.HistoryRecordTypeAssistant, "", "好的,已记住。"+bulkBody()),
		)
	}
	return records
}

// writeDistillWatermark 在 Agent 工作区写入 MEMORY.md 并把本会话的水位设为 watermark(测试辅助)。
// 直接写自动区定界注释 —— lastDistilledIndex 走的就是这条路径(与真实蒸馏同一读取面)。
func writeDistillWatermark(t *testing.T, agent dataobject.TAgent, sessionId string, watermark int) {
	t.Helper()

	path, ok := memoryDocPath(agent)
	if !ok {
		t.Fatal("应能解析 MEMORY.md 路径")
	}
	content := "<!-- auto-memory:updated=2026-09-15 10:00:00;watermarks=" + sessionId + ":" + strconv.Itoa(watermark) + " -->\n" +
		"- 既有事实:单位编码固定为 100001\n" +
		"<!-- /auto-memory -->\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入 MEMORY.md 失败: %v", err)
	}
}

// failingDistillSummarizer 让蒸馏调用失败、其余动作正常(测试辅助):
// 模拟"上层模型瞬时不可用"——此时压缩**不得**把那批未经蒸馏的原文吞掉。
func failingDistillSummarizer(calls *maintenanceModelCalls) hCommon.Summarizer {
	base := maintenanceSummarizer(calls)
	return func(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
		if modelCallTag(systemPrompt) == "distill" {
			calls.add("distill", userPrompt)
			return "", fmt.Errorf("distill upstream unavailable")
		}
		return base(ctx, systemPrompt, userPrompt)
	}
}

// TestTriggerMemoryMaintenanceCompactionClampsToDistilledRange 回归(F4):
// **被裁区间越过蒸馏水位时,压缩不得吞掉尚未蒸馏的原文**。
//
// 为什么严重:covers 会让区间内记录**同时**退出回放与蒸馏视野(见 hCommon.ActivePlainRecords),
// 顺序契约只在两者同轮触发时才生效 —— 而压缩的触发(4 条 / 400 rune)比首次蒸馏(6 轮)容易得多。
//
// 场景:水位=6、被裁区间 1-12、本轮蒸馏失败(瞬时故障)。
// 期望:压缩只覆盖已定案的 [1,6];7-12 留给下一轮蒸馏之后再压。
func TestTriggerMemoryMaintenanceCompactionClampsToDistilledRange(t *testing.T) {
	svc := newTriggerTestService(t)
	agent := newDistillAgent(t, "maintenance_clamp")

	records := maintenanceRecords(12, true)
	sessionId := cCommon.NewCompactId("maintclamp")
	fileName := writeRecords(t, &MemoryFileRWriter{nextIndex: make(map[string]int)}, sessionId, records)
	cleanupHistoryDir(t, sessionId)
	GetCompactionThrottle().Reset(sessionId)
	t.Cleanup(func() { GetCompactionThrottle().Reset(sessionId) })

	writeDistillWatermark(t, agent, sessionId, 6) // 前 6 条已蒸过,7-12 尚未
	session := dataobject.TSession{SessionId: sessionId, ChatData: fileName}

	assembled := ctxmgr.AssembleResult{
		Trimmed: true, DroppedFrom: 1, DroppedTo: len(records), DroppedRecordCount: len(records),
	}

	calls := newMaintenanceModelCalls()
	svc.TriggerMemoryMaintenance(MaintenanceRequest{
		Session: &session, Agent: &agent, Assembled: &assembled, Records: records,
		TurnSeq: 1, // 刻意不在蒸馏节拍上:本轮的蒸馏是被"压缩越界"逗出来的
	}, failingDistillSummarizer(calls))

	waitFor(t, "压缩摘要应落盘", func() bool {
		return strings.Contains(readHistoryFile(t, sessionId, fileName), "covers: 1-")
	})

	history := readHistoryFile(t, sessionId, fileName)
	if !strings.Contains(history, "covers: 1-6") {
		t.Fatalf("压缩应只覆盖已蒸馏的 1-6,实际历史文件=\n%s", history)
	}
	if strings.Contains(history, "covers: 1-12") {
		t.Fatal("压缩不得覆盖尚未蒸馏的 7-12(那段原文会永久进不了长期记忆)")
	}

	if got := tags(calls.snapshot()); len(got) == 0 || got[0] != "distill" {
		t.Fatalf("应先蒸馏(即使失败)再压缩,实际调用序列=%v", got)
	}
	if doc := readMemoryDocIfExists(t, agent); !strings.Contains(doc, sessionId+":6") {
		t.Errorf("蒸馏失败不应推进水位,实际 MEMORY.md=\n%s", doc)
	}
}

// TestTriggerMemoryMaintenanceKeepsCompactionWholeWhenWatermarkCovers 回归(F4 的反面):
// 水位已经覆盖被裁区间时必须**原样**执行压缩 —— 不额外蒸馏、不收紧区间。
// (收窄只在"压缩会越过水位"时生效,否则会无谓地延迟压缩。)
func TestTriggerMemoryMaintenanceKeepsCompactionWholeWhenWatermarkCovers(t *testing.T) {
	svc := newTriggerTestService(t)
	agent := newDistillAgent(t, "maintenance_no_clamp")

	records := maintenanceRecords(8, true)
	sessionId := cCommon.NewCompactId("maintwhole")
	fileName := writeRecords(t, &MemoryFileRWriter{nextIndex: make(map[string]int)}, sessionId, records)
	cleanupHistoryDir(t, sessionId)
	GetCompactionThrottle().Reset(sessionId)
	t.Cleanup(func() { GetCompactionThrottle().Reset(sessionId) })

	writeDistillWatermark(t, agent, sessionId, len(records)) // 水位已覆盖全部记录
	session := dataobject.TSession{SessionId: sessionId, ChatData: fileName}

	assembled := ctxmgr.AssembleResult{
		Trimmed: true, DroppedFrom: 1, DroppedTo: len(records), DroppedRecordCount: len(records),
	}

	calls := newMaintenanceModelCalls()
	svc.TriggerMemoryMaintenance(MaintenanceRequest{
		Session: &session, Agent: &agent, Assembled: &assembled, Records: records,
		TurnSeq: 1, // 不在蒸馏节拍上:本轮就不该有蒸馏调用
	}, maintenanceSummarizer(calls))

	waitFor(t, "压缩摘要应落盘", func() bool {
		return strings.Contains(readHistoryFile(t, sessionId, fileName), "covers: 1-8")
	})

	if got := tags(calls.snapshot()); len(got) != 1 || got[0] != "compact" {
		t.Fatalf("水位已覆盖被裁区间时不应额外蒸馏,实际调用序列=%v", got)
	}
}
