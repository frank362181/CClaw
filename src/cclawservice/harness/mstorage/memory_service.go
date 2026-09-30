/**
* 聊天历史管理(阶段1:双写落库 + 历史文件)
* 聊天历史的访问入口
*
* 职责(对应 data/ddl_sqlite3.sql 的三张表):
*   1. 会话建档:t_session 幂等创建,并确保历史文件 memory_{datetime}.md 存在;
*   2. 轮次落库:一轮结束后写入 t_turn(问题/回复在历史文件中的索引);
*   3. 工具落库:该轮调用过的工具逐条写入 t_turn_tool(顺序号 + 返回值摘要);
*   4. 内容落盘:用户问题 → 工具完整输出 → 助手回复,按真实发生顺序追加到历史文件。
*
* 并发:会话内 seq(轮次序号)与历史文件 index(记录序号)都是"读最大值 + 1"的分配,
* 必须串行,统一由 ctxmgr.GetSessionLocks() 按 sessionId 加锁保护。
**/

package mstorage

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	cCommon "my.favorite/cclawservice/common"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/security"
	"my.favorite/cclawservice/model/dataaccessor"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/request"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/database/core"
	"my.favorite/platformutility/logging"
	baseService "my.favorite/platformutility/webbase/middleware/service"
)

// TurnToolRecord 一次工具调用的记录(由工具循环实时填充,轮次结束时统一落库)。
type TurnToolRecord struct {
	ToolName string // 工具名
	Result   string // 工具返回值(完整内容写历史文件,DB 只存截断摘要)
	CallTime int64  // 调用时间(Unix 秒)
	RespTime int64  // 返回时间(Unix 秒);0 表示"已发起但未返回"(执行中被中断/取消)
}

// Unfinished 报告这次调用"已发起但没拿到结果"。
// 为什么要区分:中断可能落在工具执行中 —— 只记"结束"的写法会让这类调用从
// t_turn_tool 与历史文件里彻底消失,而它可能已经产生了副作用(见 M3)。
func (r TurnToolRecord) Unfinished() bool {
	return r.RespTime <= 0
}

// TurnRecorder 收集"本轮调用过的工具"。
// 必须并发安全:流式路径同一轮的多个工具是并行执行的。
type TurnRecorder struct {
	mu    sync.Mutex
	tools []TurnToolRecord
}

func NewTurnRecorder() *TurnRecorder {
	return &TurnRecorder{}
}

// Start 登记"工具开始执行",返回本次调用在快照中的下标(供 Finish 回填结果)。
//
// 两阶段(Start/Finish)而非一步到位:工具执行中被打断时,并发收尾之后的串行回填段
// 已经走不到,只记"结束"会让"这次调用发生过"在审计上完全不可见。
func (r *TurnRecorder) Start(toolName string, callTime int64) int {
	if r == nil {
		return -1
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools = append(r.tools, TurnToolRecord{
		ToolName: strings.TrimSpace(toolName),
		CallTime: callTime,
	})
	return len(r.tools) - 1
}

// Finish 回填一次已开始调用的结果;下标非法时静默忽略
// (并发下的防御,recorder 为 nil 时同样适用)。
func (r *TurnRecorder) Finish(idx int, respTime int64, result string) {
	if r == nil || idx < 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if idx >= len(r.tools) {
		return
	}

	r.tools[idx].Result = result
	r.tools[idx].RespTime = respTime
}

// Record 登记一次"不经过执行阶段"的完整调用(如脚本未注册:调用被拒绝,没有起止之分)。
// recorder 为 nil 时静默忽略,便于同步/流式共用同一份工具循环代码。
func (r *TurnRecorder) Record(toolName string, callTime, respTime int64, result string) {
	r.Finish(r.Start(toolName, callTime), respTime, result)
}

// Snapshot 返回已登记工具的快照(按登记顺序,即真实执行顺序)。
func (r *TurnRecorder) Snapshot() []TurnToolRecord {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.tools) == 0 {
		return nil
	}

	out := make([]TurnToolRecord, len(r.tools))
	copy(out, r.tools)
	return out
}

// TurnOutcome 本轮的结果维度(决定 t_turn.status / t_turn.interrupt_reason)
type TurnOutcome struct {
	Interrupted bool                       // 本轮回复残缺(被中断,或流错误导致没写完)
	Reason      dataobject.InterruptReason // 残缺来源;Interrupted=false 时应为 InterruptNone
}

// ------------------------------- end of TurnRecorder ----------------------------

// MemoryService 记忆管理服务。
type MemoryService struct {
	baseService.BaseService

	sessionAccessor dataaccessor.ISessionAccessor
	enabled         bool
}

var memoryService = pCommon.GetSingleton(func() *MemoryService {
	return &MemoryService{
		BaseService: baseService.BaseService{
			Name:        "memoryService",
			Description: "persist memory, turns and tool calls (phase 1: write-only side channel)",
		},
	}
}).Instance()

// GetMemoryService 构建记忆管理服务。
//   - enabled:来自 ini [chat] sessionRecord,关闭时本服务完全旁路(不写库、不写文件);
//   - 数据访问器构建失败时自动降级为"关闭",保证聊天主流程不受影响。
func GetMemoryService(db core.IBaseDatabase, enabled bool) *MemoryService {
	accessor, err := dataaccessor.GetSessionAccessor(db)
	if err != nil {
		logging.LogErrorF("memory service: build accessor failed, session record disabled: %s", err.Error())
		memoryService.sessionAccessor = nil
		memoryService.enabled = false
		return memoryService
	}

	memoryService.sessionAccessor = accessor
	memoryService.enabled = enabled
	return memoryService
}

// Enabled 报告本服务是否生效(供调用方跳过无谓的记录开销)。
func (s *MemoryService) Enabled() bool {
	return s.usable()
}

// ErrSessionOwnershipConflict 会话已属于另一个 sender:sender 是当前唯一可用的归属凭据,
// 不一致时不能把本轮问答写进别人的会话与历史文件。
//
// 为什么要作为一个可区分的原因返回(而不是只返回 ok=false):
// 静默丢弃的表现是“每轮都答得出、历史永远为空”,用户与运维都无法定位(见 A2 / M4)。
// 调用方据此选择“告知用户”而不是默默承受 —— 但它**不是致命错误**,记忆是旁路,
// 调用方仍应继续聊天流程。
var ErrSessionOwnershipConflict = errors.New("memory service: session belongs to another sender")

// Begin 会话建档(幂等):不存在则创建 t_session + 历史文件,已存在则复用并按需补齐历史文件名。
//
// 返回值:
//   - ok=true:记录可用,session 有效(新建或复用成功);
//   - ok=false:记录不可用,调用方应继续聊天流程(记忆是旁路,不阻断对话);
//   - err 非 nil 时给出**可区分**的不可用原因(目前只有 ErrSessionOwnershipConflict)。
func (s *MemoryService) Begin(sender string, agent dataobject.TAgent, req *request.ChatRequest) (dataobject.TSession, bool, error) {
	if !s.usable() || req == nil {
		return dataobject.TSession{}, false, nil
	}

	sessionId := cCommon.NormalizeCompactId(req.SessionId)
	if sessionId == "" {
		logging.LogWarnF("memory service: empty session id, skip record")
		return dataobject.TSession{}, false, nil
	}

	unlock := GetStorageLocks().Lock(sessionId)
	defer unlock()

	now := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime()

	// 1. 会话已存在 → 复用(常见路径:会话的第 2..n 条消息)
	existing, err := s.sessionAccessor.GetSession(sessionId)
	if err != nil {
		logging.LogWarnF("memory service: load session %s error:%s", sessionId, err.Error())
		return dataobject.TSession{}, false, nil
	}
	if existing != nil {
		// sender 是唯一可用的归属凭据。若不一致,说明客户端在复用他人会话 ——
		// 必须拒绝记录,否则会把 B 的问答追加进 A 的会话与历史文件。
		if !strings.EqualFold(strings.TrimSpace(sender), strings.TrimSpace(existing.Sender)) {
			logging.LogErrorF("memory service: session %s belongs to sender %q but the request comes from %q, this turn will not be recorded",
				existing.SessionId, existing.Sender, strings.TrimSpace(sender))
			return dataobject.TSession{}, false, ErrSessionOwnershipConflict
		}
		return s.ensureMemoryFile(sender, sessionId, *existing, now), true, nil
	}

	// 2. 首条消息 → 建档
	session := dataobject.TSession{
		SessionId:      sessionId,
		SessionName:    hCommon.BuildSessionName(req.ReqMsg),
		AgentId:        strings.TrimSpace(agent.AgentId),
		BeginTimestamp: now.Timestamp,
		ChatType:       req.ChatType,
		ChatChannel:    req.ChatChannel,
		Sender:         strings.TrimSpace(sender),
	}

	// 先落历史文件:文件名要一并写进 t_session.chat_data
	fileName, fileErr := GetMemoryFileWriter().Ensure(sender, sessionId, now, s.memoryFileHeaderLines(session, now))
	if fileErr != nil {
		// 文件失败不阻断建档:chat_data 留空,后续轮次会再尝试补齐
		logging.LogWarnF("memory service: create history file for %s error:%s", sessionId, fileErr.Error())
	} else {
		session.ChatData = fileName
	}

	if err = s.sessionAccessor.CreateSession(&session); err != nil {
		// 并发下可能已被其它请求建档(唯一键冲突):回读一次,复用先到者
		if again, loadErr := s.sessionAccessor.GetSession(sessionId); loadErr == nil && again != nil {
			return s.ensureMemoryFile(sender, sessionId, *again, now), true, nil
		}
		logging.LogWarnF("memory service: create session %s error:%s", sessionId, err.Error())
		return dataobject.TSession{}, false, nil
	}

	return session, true, nil
}

// AppendTurn 记录一轮:写入 t_turn + t_turn_tool,并把内容追加到历史文件。
// reply 为最终回复文本,tools 为本轮调用过的工具(可为空)。
//
// 返回本轮分配的会话内轮次序号(实际记下的序号;未记录/失败返回 0)。
// 调用方(记忆维护)据此判断是否需要蒸馏 —— 避免再跑一次 SELECT MAX(seq)。
func (s *MemoryService) AppendTurn(sess dataobject.TSession, req *request.ChatRequest, reply string, tools []TurnToolRecord) int {
	return s.appendTurn(sess, req, reply, tools, TurnOutcome{})
}

// AppendInterruptedTurn 记录一轮**残缺**的轮次(用户点中断/客户端断开/运行超时/流错误)。
func (s *MemoryService) AppendInterruptedTurn(sess dataobject.TSession, req *request.ChatRequest, partial string, tools []TurnToolRecord, reason dataobject.InterruptReason) int {
	seq := s.appendTurn(sess, req, partial, tools, TurnOutcome{Interrupted: true, Reason: reason})
	logging.LogInfoF("chat session service: interrupted turn recorded, session=%s turn=%s reason=%s partial_chars=%d seq=%d",
		sess.SessionId, req.TurnId, reason, len([]rune(partial)), seq)
	return seq
}

// ReadRecords 读回某会话的历史记录(供"上下文读回装配")。
//
// 语义:
//   - 旁路读:任何失败都按"无历史"处理,由调用方决定是否降级,绝不阻断本轮对话;
//   - 会话尚未建档(chat_data 为空)时返回空,不是错误。
func (s *MemoryService) ReadRecords(session dataobject.TSession) ([]hCommon.MemoryRecord, error) {
	if !s.fileStorageUsable() {
		return nil, nil
	}

	fileName := strings.TrimSpace(session.ChatData)
	if fileName == pCommon.BlankString {
		return nil, nil
	}
	return GetMemoryFileWriter().Read(session.Sender, session.SessionId, fileName)
}

// TriggerMemoryMaintenance 触发后台记忆维护(旁路、异步):按需蒸馏、按需压缩、按需合并摘要。
//
// 为什么合并成一个后台任务:
//
//	三者都是"耗一次 LLM 调用、与用户回复无关"的记忆维护动作,合并后同一会话只起一个协程,
//	并共享同一次读回结果,避免重复 IO。
//
// **执行顺序是契约** —— 蒸馏必须在压缩之前(否则被压缩的原文对蒸馏永久不可见),
// 顺序由 maintenancePlan.steps 给出并单测锁住(memory_maintenance_test.go 锁声明,
// memory_maintenance_trigger_test.go 锁"真跑起来之后的调用序列与输入")。
func (s *MemoryService) TriggerMemoryMaintenance(maintenanceReq MaintenanceRequest, summarizer hCommon.Summarizer) {

	if s == nil || !s.Enabled() {
		return
	}

	// **这个函数跑在调用方协程上**(聊天/结束会话的主流程),后台协程里的 recover 兜不住它 ——
	// 所以每个解引用都必须先过守卫:旁路增强不得把主流程打崩。
	if maintenanceReq.Session == nil || maintenanceReq.Agent == nil {
		logging.LogWarnF("memory service: memory maintenance skipped, session or agent is nil")
		return
	}
	// 收尾路径(EndChatSession)没有"本轮装配结果",传 nil:归一为"无裁剪 + 预算未耗尽"的零值。
	if maintenanceReq.Assembled == nil {
		zero := hCommon.AssembleResult{}
		maintenanceReq.Assembled = &zero
	}

	// 解引用一次,后续读起来与"动作"部分的 session/agent 保持同一个名字
	session, agent := maintenanceReq.Session, maintenanceReq.Agent
	if strings.TrimSpace(session.ChatData) == pCommon.BlankString {
		return
	}

	// 预算为 0:窗口扣掉输出预留与系统提示预留后没有剩余,压缩改变不了这一点,
	// 反复压缩只会白烧 LLM 调用。这里明确告警并把问题指向配置,而不是继续压
	if maintenanceReq.Assembled.BudgetExhausted {
		logging.LogWarnF("memory service: history budget is exhausted for session=%s (context_window - output reserve - max(est(system), reverse_token_floor) <= 0); compaction disabled, check the agent/model config",
			session.SessionId)
	}

	// 压缩的**收益**维度:被裁区间至少要有 MinCompactionSrcRunes 的体积才值得一次 LLM 调用。
	// 早退扫描(够门槛就停),records 是装配刚读回的,请求路径上不额外读文件。
	droppedRunes := hCommon.RecordsRunesInRange(maintenanceReq.Records, maintenanceReq.Assembled.DroppedFrom, maintenanceReq.Assembled.DroppedTo,
		hCommon.MinCompactionSrcRunes)
	needCompact := security.NeedCompact(*maintenanceReq.Assembled, droppedRunes)
	// 请求路径上只取一次"当前时刻",压缩与合并两处节流共用
	now := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp

	// 冷却期内直接不起后台任务:省掉协程 + 一次全量读回(Compact 内部还会再判一次,以那里为准)
	if needCompact && !GetCompactionThrottle().Allow(session.SessionId, now) {
		needCompact = false
	}

	// 轮次数直接取"本轮刚落库的序号" —— AppendTurn 已经查过一次 MAX(seq),
	// 这里不再重复查询(它在请求路径上、且在本函数的所有早退判断之前)。
	// 只有本轮未被记录(建档失败/幂等跳过)时才退回一次查询:那时序号没有推进。
	turns := maintenanceReq.TurnSeq
	if turns <= 0 {
		turns = s.TurnCount(session.SessionId)
	}

	// 蒸馏的**内容**维度:水位之后还剩多少(够一轮就提前蒸,不必等节拍)。
	// 压力只在内容检查节拍上才影响判定(见 security.NeedDistillPressure):
	// 其余轮次只需节拍,省掉这次 MEMORY.md 读取 + 扫描。
	//
	// 压缩判定同样需要水位(见下面的压缩上界),因此两处**共用一次读取**。
	needPressure := security.NeedDistillPressure(turns)
	watermark, watermarkKnown := 0, false
	if needPressure || needCompact {
		watermark, watermarkKnown = s.distillWatermark(*agent, session.SessionId)
	}

	pressure := security.NewDistillPressure(maintenanceReq.Closing)
	if needPressure && watermarkKnown {
		pressure.PendingRecords, pressure.PendingChars = distillPressureAt(watermark, maintenanceReq.Records)
	}
	needDistill := security.NeedDistill(turns, pressure)

	// 摘要总量超上限 → 合并最旧的一批摘要(判定见 security.NeedMerge,与压缩的预算维度同源)。
	needMerge := security.NeedMerge(*maintenanceReq.Assembled)

	// 【维护预算按窗口收口】
	//
	// 三个动作都要调用模型,而它们的源文本上限(20000/24000 rune)是"看着大模型定的"常量:
	// 换成小窗模型会把请求顶出窗口,而失败只发生在后台日志里(前端完全看不到)。
	// 这里按 (窗口 - 维护输出额度 - 系统提示估量) 收窄,**装不下就不排这个动作**,
	// 并把问题指向配置(面向运维的告警见 security.PolicyWarnings,每 agent 只记一次)。
	// 窗口未知(ContextWindow<=0)时退化为硬上限,行为与收口前完全一致。
	budgets := security.CalcMaintenanceBudgets(maintenanceReq.ContextWindow, distillSystemPrompt, compactionSystemPrompt, compactionMergeSystemPrompt)
	if needDistill && budgets.DistillBudget <= 0 {
		logging.LogDebugF("memory service: session=%s distill skipped, the window (%d) leaves no room for a maintenance call",
			session.SessionId, maintenanceReq.ContextWindow)
		needDistill = false
	}
	if needCompact && budgets.CompactBudget <= 0 {
		logging.LogDebugF("memory service: session=%s compact skipped, the window (%d) leaves no room for a maintenance call",
			session.SessionId, maintenanceReq.ContextWindow)
		needCompact = false
	}
	if needMerge && budgets.MergeBudget <= 0 {
		logging.LogDebugF("memory service: session=%s merge skipped, the window (%d) leaves no room for a maintenance call",
			session.SessionId, maintenanceReq.ContextWindow)
		needMerge = false
	}

	// 【压缩不得吞掉"尚未蒸馏"的原文】
	compactCeiling := -1 // <0 表示"本轮无需收紧压缩区间"
	if needCompact && watermarkKnown && maintenanceReq.Assembled.DroppedTo > watermark {
		compactCeiling = watermark
		// 补蒸同样受窗口预算约束:装不下就不强求 —— 此时压缩仍会被钳在水位处,
		// 只是要等下一次蒸馏(会按同样的规则重试)。
		needDistill = budgets.DistillBudget > 0
		logging.LogDebugF("memory service: session=%s compaction range %d-%d reaches beyond the distill watermark %d, distill first (compaction may clamp, distill=%v)",
			session.SessionId, maintenanceReq.Assembled.DroppedFrom, maintenanceReq.Assembled.DroppedTo, watermark, needDistill)
	}

	// 与压缩同理:冷却期内直接不起后台任务。
	// 合并的触发条件(摘要超预算)一旦成立就是**持续为真**,没有冷却会退化成每轮一次 LLM 调用;
	// 键与压缩分开,见 mergeThrottleKey。
	if needMerge && !GetCompactionThrottle().Allow(mergeThrottleKey(session.SessionId), now) {
		needMerge = false
	}

	if !needCompact && !needDistill && !needMerge {
		return
	}

	// 压缩模式来源:t_agent.compaction_mode —— 0=default(整体摘要) / 1=safeguard(分块+保全),
	// 常量与名称见 dataobject.CompactionMode*,模式差异集中在 mstorage/compressor.go。
	from, to, mode := maintenanceReq.Assembled.DroppedFrom, maintenanceReq.Assembled.DroppedTo, agent.CompactionMode

	// 获得一个维护routine，避免无限次获取打满LLM
	if !hCommon.AcquireMaintenanceSlot() {
		logging.LogDebugF("memory service: memory maintenance slot unavailable, skip session=%s", session.SessionId)
		return
	}

	// 判定阶段结束:入参里的记录快照只服务于判定(见 MaintenanceRequest.Records),
	// 显式释放,避免后台协程在 CompactionRunTimeout 内白白拖住整个切片。
	maintenanceReq.Records = nil
	plan := maintenancePlan{distill: needDistill, compact: needCompact, merge: needMerge}

	go func() {
		defer hCommon.ReleaseMaintenanceSlot()
		defer func() { // 后台协程必须自兜 panic:否则单个会话的异常会带崩整个进程
			if r := recover(); r != nil {
				logging.LogErrorF("memory service: panic in memory maintenance of %s: %v", session.SessionId, r)
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), hCommon.CompactionRunTimeout*time.Second)
		defer cancel()

		// 重读一份**最新**记录供各动作使用:判定用的请求路径快照省一次 IO,
		// 动作必须用重读结果 —— 上面每个判定都已在获槽前完成,之后才排队。
		records, err := s.ReadRecords(*session)
		if err != nil {
			logging.LogWarnF("memory service: read records for memory maintenance error:%s", err.Error())
			return
		}

		// 动作顺序见 maintenancePlan.steps:蒸馏 → 压缩 → 合并摘要。
		// 先蒸馏再压缩,两个目标都达到:长期记忆拿到完整原文,历史体积随后有界。
		//
		// compactCeiling >= 0 表示请求路径发现"被裁区间越过了水位":此时压缩上界先收在水位处,
		// 再由蒸馏结果把它往前推(蒸馏本轮**判定过**的区间可以放心交给压缩覆盖)。
		compactLimit := compactCeiling
		for _, step := range plan.steps() {
			switch step {
			case stepDistill:
				// 与压缩共用同一份重读结果:本轮的追加记录不能漏蒸(用请求路径的旧快照会漏)
				distilled, distillErr := s.distillWithBudget(ctx, *session, records, *agent, summarizer, budgets.DistillBudget)
				if distillErr != nil {
					logging.LogWarnF("memory service: distill memory of %s error:%s", session.SessionId, distillErr.Error())
				}
				// CoversTo 非零 = 这批原文已被蒸馏"定案"(送进模型,或本地预筛判定无长期价值)。
				// 失败/未选批时为 0 —— 那就保持在水位上,压缩本轮不越过它。
				if compactCeiling >= 0 && distilled.CoversTo > compactLimit {
					compactLimit = distilled.CoversTo
				}

			case stepCompact:
				compactTo := to
				if compactLimit >= 0 && compactTo > compactLimit {
					// 被裁区间里有一部分还没被蒸馏定案:先压水位以内的部分,
					// 其余留给下一轮蒸馏之后再压(留着的记录本来也已不在回放窗口里,不额外损失)。
					logging.LogWarnF("memory service: session=%s compact range clamped to %d-%d (asked %d-%d, beyond the distilled range)",
						session.SessionId, from, compactLimit, from, to)
					compactTo = compactLimit
				}
				if compactTo < from {
					logging.LogDebugF("memory service: session=%s compact deferred: %d-%d is not distilled yet",
						session.SessionId, from, to)
					break
				}
				if _, err = s.compactWithBudget(ctx, *session, records, from, compactTo, mode, summarizer, budgets.CompactBudget); err != nil {
					logging.LogWarnF("memory service: compact history of %s error:%s", session.SessionId, err.Error())
				}

			case stepMerge:
				// 合并必须基于**最新**内容:压缩刚写完摘要,摘要集合与总量都变了。
				// (蒸馏只动 MEMORY.md,不改历史文件,因此无需为它重读。)
				if latest, readErr := s.ReadRecords(*session); readErr == nil {
					records = latest
				} else {
					logging.LogWarnF("memory service: re-read records before merge of %s error:%s",
						session.SessionId, readErr.Error())
				}

				// 以**重读后**的判定为准:压缩本身也可能把摘要总量推过阈值。
				if security.SummaryOverBudget(records) {
					if _, err = s.mergeWithBudget(ctx, *session, records, summarizer, budgets.MergeBudget); err != nil {
						logging.LogWarnF("memory service: merge summaries of %s error:%s", session.SessionId, err.Error())
					}
				}
			}
		}
	}()
}

// TurnCount 返回会话已记录的轮次数(供后台记忆维护做节流判断;失败返回 0)。
func (s *MemoryService) TurnCount(sessionId string) int {
	if !s.usable() {
		return 0
	}
	sessionId = cCommon.NormalizeCompactId(sessionId)
	if sessionId == "" {
		return 0
	}

	count, err := s.sessionAccessor.GetMaxTurnSeq(sessionId)
	if err != nil {
		logging.LogWarnF("chat session service: count turns of %s error:%s", sessionId, err.Error())
		return 0
	}
	return count
}

// Compact 把 [from, to] 区间内的记录压缩为一条摘要记录。
//
// 导出入口用**硬上限**作为源文本预算(等价于窗口未知);编排侧走 compactWithBudget,
// 把按窗口算出的预算传进来。
func (s *MemoryService) Compact(ctx context.Context, session dataobject.TSession, records []hCommon.MemoryRecord,
	from, to, mode int, summarize hCommon.Summarizer) (CompactionResult, error) {
	return s.compactWithBudget(ctx, session, records, from, to, mode, summarize, hCommon.MaxCompactionInputChars)
}

// CompactSummaries 把最旧的一批摘要合并为一条摘要(B2:摘要总量超预算时调用)。
//
// 导出入口用**硬上限**作为源文本预算;编排侧走 mergeWithBudget(按窗口收口)。
//
// 与 Compact 的区别:输入是**摘要正文**而不是原始记录。合并后的 covers 区间刻意包含
// 被合并摘要的记录序号,这样读取端会把它们判定为"已被取代",从而真正缩小注入体积。
//
// 注意:合并**不区分 compaction_mode**(它是摘要自身的整理,不是"怎么摘要原始记录"),
// 因此结果里 Mode 恒为 default、Degraded 恒为 false;日志与结果里的 Merged>0 是唯一标识。
//
// 节流:与压缩共用同一张表但**分键**(mergeThrottleKey)。合并的触发条件(摘要超预算)
// 一旦成立就是持续为真,而它的失败原因也往往是持续状态 —— 没有冷却会退化成每轮一次
// LLM 调用/全量挑选,因此每条"没取得进展"的出口都要计入冷却。
//
// 返回 SummaryIndex=0 表示没有可安全合并的集合(宁可超预算,也不写过度承诺的 covers)。
func (s *MemoryService) CompactSummaries(ctx context.Context, session dataobject.TSession,
	records []hCommon.MemoryRecord, summarize hCommon.Summarizer) (CompactionResult, error) {
	return s.mergeWithBudget(ctx, session, records, summarize, hCommon.MaxCompactionInputChars)
}

// DistillPressure 汇总"内容侧"的蒸馏压力:水位之后还剩多少没蒸(供调用方决定要不要发蒸馏)。
//
// 数据来源都是调用方已经有的东西:records(装配刚读过)+ MEMORY.md 的水位(<=12KB 的小文件)。
// 文字长度按"够一轮就早退"扫,不做全量统计 —— 这个判断在请求路径上,必须便宜。
//
// 口径刻意比 Distill 的输入**宽**一点(只按水位切,不再排除被摘要代表的记录):
// 那是为了省掉每请求一次的 ActivePlainRecords(建 map + 全量扫),
// 而精确口径由 Distill 内部再卡一次 —— 多触发一次只会被它内部拦住,代价极小。
//
// 本方法是"只想拿压力"时的独立入口(含一次文件读取);
// 编排侧在压缩判定也需要水位时会改用 distillWatermark + distillPressureAt,
// 让两者共用同一次读取。
func (s *MemoryService) DistillPressure(session dataobject.TSession, agent dataobject.TAgent,
	records []hCommon.MemoryRecord) (pendingRecords, pendingChars int) {
	if !s.fileStorageUsable() || len(records) == 0 {
		return 0, 0
	}

	watermark, ok := s.distillWatermark(agent, session.SessionId)
	if !ok {
		return 0, 0 // 水位未知(读不到文件):按"无压力"处理,不因一次读失败就多发一次蒸馏
	}
	return distillPressureAt(watermark, records)
}

// Distill 把会话记录蒸馏为长期记忆并写入 Agent 工作区的 MEMORY.md。
//
// 导出入口用**硬上限**作为源文本预算;编排侧走 distillWithBudget(按窗口收口)。
func (s *MemoryService) Distill(ctx context.Context, session dataobject.TSession, records []hCommon.MemoryRecord,
	agent dataobject.TAgent, summarize hCommon.Summarizer) (DistillResult, error) {
	return s.distillWithBudget(ctx, session, records, agent, summarize, hCommon.DistillMaxInputChars)
}

// EndSession 结束会话(供后续阶段:空闲超时/手动结束/触发 compact 调用)。
// 阶段1 不主动调用:结束语义属于会话生命周期管理,留待接入空闲超时与压缩时统一编排。
func (s *MemoryService) EndSession(sessionId string, reason int) error {
	if !s.usable() {
		return nil
	}

	sessionId = cCommon.NormalizeCompactId(sessionId)
	if sessionId == "" {
		return nil
	}

	now := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime()
	return s.sessionAccessor.SetSessionEnded(sessionId, reason, now.Timestamp)
}

// ============================== private functions & methods ==============================

// reloadForCompaction 在**持锁之后**重新读回记录(失败则退回调用方传入的快照)。
//
// 为什么要重读:调用方的快照是"读锁之前"的,看不到等锁期间别的协程已经追加的摘要,
// 用它做"区间是否已被压缩"的判定会对同一区间写出重复摘要(两条摘要互相不构成取代,内容会重复注入)。
// Read 有增量缓存(头指纹 + parsedOffset),文件只追加时只解析新增字节,代价很低。
func (s *MemoryService) reloadForCompaction(session dataobject.TSession, fileName string,
	fallback []hCommon.MemoryRecord) []hCommon.MemoryRecord {
	latest, err := GetMemoryFileWriter().Read(session.Sender, session.SessionId, fileName)
	if err != nil {
		logging.LogDebugF("chat memory compact: reload records of %s error:%s (fallback to caller snapshot)",
			session.SessionId, err.Error())
		return fallback
	}
	return latest
}

// usable 报告记录能力是否可用(已启用 + 数据访问器就绪)。
func (s *MemoryService) usable() bool {
	return s != nil && s.enabled && s.sessionAccessor != nil
}

// fileStorageUsable 报告"文件侧能力"是否可用。
// 压缩/蒸馏/读回只读写历史文件与长期记忆文件,不依赖 DB 访问器;
// 与 usable 分开判定,避免无 DB 的场景把文件能力一并禁掉(也让单测无需搭库)。
func (s *MemoryService) fileStorageUsable() bool {
	return s != nil && s.enabled
}

// ensureMemoryFile 保证会话已有历史文件:chat_data 为空(此前创建失败)时补建并回写。
func (s *MemoryService) ensureMemoryFile(sender, sessionId string, session dataobject.TSession, now *pCommon.DatetimeItem) dataobject.TSession {
	if strings.TrimSpace(session.ChatData) != "" {
		return session
	}

	fileName, err := GetMemoryFileWriter().Ensure(sender, sessionId, now, s.memoryFileHeaderLines(session, now))
	if err != nil {
		logging.LogWarnF("memory service: ensure history file for %s error:%s", sessionId, err.Error())
		return session
	}

	if err = s.sessionAccessor.SetSessionChatData(sessionId, fileName); err != nil {
		logging.LogWarnF("memory service: set chat_data of %s error:%s", sessionId, err.Error())
	}
	session.ChatData = fileName
	return session
}

// turnStatusOf 计算 t_turn.status(纯函数,便于单测;取值见 dataobject.TurnStatus*)。
//
// 优先级:有助手记录 > 中断且设计上不写 > 其余(无历史文件 / 写失败)。
// 后两种都归 3,靠 chat_index 区分 —— chat_index==0 说明整轮都没进文件(会话当时无历史文件)。
func turnStatusOf(interrupted bool, hasHistoryFile bool, respIndex int, appendFailed bool) int {
	switch {
	case respIndex > 0:
		if interrupted {
			return dataobject.TurnStatusInterruptedPartial
		}
		return dataobject.TurnStatusNormal
	case interrupted && hasHistoryFile && !appendFailed:
		return dataobject.TurnStatusInterruptedEmpty
	default:
		return dataobject.TurnStatusRespNotWritten
	}
}

// interruptReasonOf 归一落库来源:status 为 1/2 的轮次必须有非零来源,否则按来源聚合时会漏。
func interruptReasonOf(outcome TurnOutcome) dataobject.InterruptReason {
	if !outcome.Interrupted {
		return dataobject.InterruptNone
	}
	if outcome.Reason == dataobject.InterruptNone {
		return dataobject.InterruptOther // 兜底:残缺但没给来源 ⇒ 归为其它错误
	}
	return outcome.Reason
}

// appendTurn 轮次记录的公共实现;outcome.Interrupted=true 时助手记录带残缺标记,并落到 t_turn.status。
// 返回本轮落库的会话内轮次序号(未记录/失败返回 0)。
func (s *MemoryService) appendTurn(sess dataobject.TSession, req *request.ChatRequest, reply string, tools []TurnToolRecord, outcome TurnOutcome) int {
	if !s.usable() || req == nil {
		return 0
	}

	sessionId := cCommon.NormalizeCompactId(sess.SessionId)
	if sessionId == "" {
		return 0
	}

	unlock := GetStorageLocks().Lock(sessionId)
	defer unlock()

	// 1. 轮次标识:优先用客户端传入的 turnId —— 它与"中断"、"应答回显"用的是同一个标识,
	//    落库保持一致,客户端才能定位本轮。仅当旧版前端未提供时才由服务端补一个
	//    (不带前缀:turnId 不含 sender,跨用户隔离只在 BuildTurnStreamKey 上)。
	turnId := cCommon.NormalizeCompactId(req.TurnId)
	if turnId == pCommon.BlankString {
		turnId = cCommon.NewCompactId(pCommon.BlankString)
	}

	// 2. 幂等:同一 turnId 重复上报(客户端重试/重复提交)直接跳过。
	//    必须早于历史文件追加,否则同一轮问答会被写两次并白占文件序号。
	if existing, err := s.sessionAccessor.GetTurn(turnId); err == nil && existing != nil {
		logging.LogWarnF("memory service: duplicated turn %s of session %s, skip", turnId, sessionId)
		return 0
	}

	// 3. 分配会话内轮次序号(持锁下"读最大值 + 1")
	maxSeq, err := s.sessionAccessor.GetMaxTurnSeq(sessionId)
	if err != nil {
		logging.LogWarnF("memory service: max turn seq of %s error:%s", sessionId, err.Error())
		return 0
	}

	// 4. 分配历史记录序号:权威来源是 DB(t_turn.resp_index 的最大值 + 1)
	maxRespIndex, err := s.sessionAccessor.GetMaxRespIndex(sessionId)
	if err != nil {
		logging.LogWarnF("memory service: max resp index of %s error:%s", sessionId, err.Error())
		return 0
	}
	nextIndex := maxRespIndex + 1

	now := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime()
	// 提问时间取"请求到达时刻"(normalizeRequest 已兜底写入 req.GenAt);
	// resp_time 取本轮结束时刻,二者之差即响应耗时。
	// 此处保留下限兜底,仅防未经 normalizeRequest 的调用方。
	chatTime := req.GenAt
	if chatTime <= 0 {
		chatTime = now.Timestamp
	}

	fileName := strings.TrimSpace(sess.ChatData)
	writer := GetMemoryFileWriter()

	// 5. 历史文件:按真实发生顺序写入 问题 → 工具 → 回复。
	//    每条都带上期望序号(从 DB 推出的 nextIndex 起递增),
	//    写入器会取"DB 序号与本进程缓存"的最大值,保证序号只增不减(见写入器的序号契约)。
	chatIndex := 0
	if fileName != "" {
		if idx, err := writer.Append(sess.Sender, sessionId, fileName, hCommon.HistoryRecordTypeUser, "", req.ReqMsg, pCommon.TimestampText(chatTime), nextIndex); err == nil {
			chatIndex = idx
		} else {
			logging.LogWarnF("memory service: append user record error:%s", err.Error())
		}
	}

	toolRows := make([]dataobject.TTurnTool, 0, len(tools))
	for i, tool := range tools {
		result := tool.Result
		if tool.Unfinished() {
			// 已发起但没拿到结果(执行中被中断/取消/超时):不能留空 ——
			// 调用已经发生且可能已有副作用,"这次调用发生过"本身就是审计信息(见 M3)。
			result = hCommon.TurnToolUnfinishedResult
			logging.LogWarnF("memory service: tool %s of turn %s started but never returned a result, recorded as unfinished", tool.ToolName, turnId)
		}
		toolRows = append(toolRows, dataobject.TTurnTool{
			Seq:        i + 1,
			ToolCalled: true,
			CallTime:   tool.CallTime,
			ToolName:   tool.ToolName,
			ToolResp:   pCommon.TruncateRunes(strings.TrimSpace(result), hCommon.TurnToolRespMaxLen, pCommon.BlankString), //写入数据库时截断，仅仅写前256 runes
			RespTime:   tool.RespTime,
		})
		if fileName != "" {
			if _, err := writer.Append(sess.Sender, sessionId, fileName, hCommon.HistoryRecordTypeTool, tool.ToolName, result, pCommon.TimestampText(tool.CallTime), nextIndex+1+i); err != nil {
				logging.LogWarnF("memory service: append tool record error:%s", err.Error())
			}
		}
	}

	respIndex := 0
	appendFailed := false
	assistantTitle := pCommon.BlankString
	if outcome.Interrupted {
		assistantTitle = hCommon.HistoryRecordTypeInterrupted
	}
	// 中断且没有任何产出时不写助手记录:空记录无信息量,徒增噪声(resp_index 保持 0)
	if fileName != "" && (!outcome.Interrupted || strings.TrimSpace(reply) != pCommon.BlankString) {
		if idx, err := writer.Append(sess.Sender, sessionId, fileName, hCommon.HistoryRecordTypeAssistant, assistantTitle, reply, pCommon.TimestampText(now.Timestamp), nextIndex+1+len(tools)); err == nil {
			respIndex = idx
		} else {
			appendFailed = true
			logging.LogWarnF("memory session service: append assistant record error:%s", err.Error())
		}
	}

	status := turnStatusOf(outcome.Interrupted, fileName != "", respIndex, appendFailed)

	// 6. 落库轮次(turn_id 用客户端传入值,t_turn.turn_id 唯一约束作为最后一道幂等防线)
	turn := dataobject.TTurn{
		TurnId:          turnId,
		SessionId:       sessionId,
		Seq:             maxSeq + 1,
		ChatTime:        chatTime,
		ChatIndex:       chatIndex,
		RespTime:        now.Timestamp,
		RespIndex:       respIndex,
		Status:          status,
		InterruptReason: interruptReasonOf(outcome),
	}
	if err = s.sessionAccessor.CreateTurn(&turn); err != nil {
		logging.LogWarnF("chat session service: create turn of %s error:%s", sessionId, err.Error())
		return 0
	}

	// 7. 落库轮内工具
	for i := range toolRows {
		toolRows[i].TurnId = turn.TurnId
	}
	if err = s.sessionAccessor.CreateTurnTools(toolRows); err != nil {
		logging.LogWarnF("chat session service: create turn tools of %s error:%s", turn.TurnId, err.Error())
	}

	return turn.Seq
}

// memoryFileHeaderLines 构造历史文件头部的元数据行。
func (s *MemoryService) memoryFileHeaderLines(session dataobject.TSession, now *pCommon.DatetimeItem) []string {
	beginText := pCommon.TimestampText(session.BeginTimestamp)
	if now != nil && session.BeginTimestamp <= 0 {
		beginText = now.DatetimeStr
	}

	return []string{
		"session_id: " + session.SessionId,
		"session_name: " + session.SessionName,
		"agent_id: " + session.AgentId,
		"sender: " + session.Sender,
		"begin: " + beginText,
	}
}

// compactWithBudget 与 Compact 相同,但源文本预算由调用方给定(见 ctxmgr.MaintenanceBudgets)。
//
//   - session:   所属会话(chat_data 即历史文件名)
//   - records:   调用方读回的快照(会在持锁后重新读一次,见 reloadForCompaction)
//   - from/to:   待压缩的记录序号闭区间(来自装配器报告的"被裁掉的区间")
//   - mode:      dataobject.CompactionModeDefault / CompactionModeSafeguard
//   - summarize: 摘要生成器(注入 LLM 调用)
//   - budget:    送入模型的源文本预算(rune)
//
// 返回值:压缩结果;未执行压缩时 SummaryIndex 为 0 且 err 为 nil(属正常情况,非错误)。
func (s *MemoryService) compactWithBudget(ctx context.Context, session dataobject.TSession, records []hCommon.MemoryRecord,
	from, to, mode int, summarize hCommon.Summarizer, budget int) (CompactionResult, error) {
	result := CompactionResult{Mode: mode}
	if !s.fileStorageUsable() || summarize == nil {
		return result, nil
	}

	fileName := strings.TrimSpace(session.ChatData) //聊天记录的文件
	if fileName == pCommon.BlankString || from <= 0 || to < from {
		return result, nil
	}

	// 同会话压缩串行:压缩会读全量记录再追加摘要,并发执行会写出重复摘要。
	// 用专用锁键(与历史写入锁不同名,避免与 Append 的内部加锁自锁)。
	unlock := GetStorageLocks().Lock(hCommon.BuildCompactionLockKey(session.SessionId))
	defer unlock()

	records = s.reloadForCompaction(session, fileName, records)
	now := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp

	// 目标:区间内连续、尚未被摘要覆盖、且不含摘要记录的一段普通记录
	run := compactionRun(records, from, to)
	if len(run) < hCommon.CompactionMinRecords {
		// 没活干(区间已被摘要代表/可压段太短):同样计入冷却。
		// 否则停滞的会话会每一轮都起协程 + 整文件读回,而冷却永远不会启动。
		GetCompactionThrottle().RecordFailure(session.SessionId, now)
		return result, nil
	}

	// 收缩区间至送入模型的预算之内:宁可这次少压一段,也不能宣称覆盖没送进模型的记录
	run, fit := fitCompactionRange(run, budget)
	if fit < hCommon.CompactionMinRecords {
		logging.LogDebugF("chat memory compact: session=%s range too small after fitting the input budget (%d records)", session.SessionId, fit)
		GetCompactionThrottle().RecordFailure(session.SessionId, now)
		return result, nil
	}
	coverFrom, coverTo := run[0].Index, run[len(run)-1].Index

	// 收益门槛:压缩的价值来自"变小"。源体积不大于摘要上限时压缩只有坏处
	// (摘要会折进 system 消息,而 system 越长历史预算越小 —— 会形成正反馈)。
	srcRunes := hCommon.RecordsRunes(run)
	summaryCap := effectiveSummaryCap(srcRunes)
	if srcRunes <= summaryCap {
		logging.LogDebugF("chat memory compact: session=%s skipped, no gain (src_runes=%d <= summary_cap=%d)",
			session.SessionId, srcRunes, summaryCap)
		GetCompactionThrottle().RecordFailure(session.SessionId, now)
		return result, nil
	}

	// 节流:一次尝试后进入冷却(连续降级按 2 倍退避)。
	// 目的:保全校验失败时"后半原文下一轮又被裁 → 又压缩"不得退化成每轮一次 LLM 调用。
	// 位置很关键:必须在"确实要发起尝试"之后才判 —— 区间已被覆盖/太短都不算尝试,不消耗冷却。
	if !GetCompactionThrottle().Allow(session.SessionId, now) {
		logging.LogDebugF("chat memory compact: session=%s in cooldown, skip attempt", session.SessionId)
		return result, nil
	}

	// 【模式差异 1/3】摘要生成:default=整段一次;safeguard=分块多次(见 buildSummaryByMode)
	summaryText, err := buildSummaryByMode(ctx, run, coverFrom, coverTo, mode, summarize)
	if err != nil {
		GetCompactionThrottle().RecordFailure(session.SessionId, now) // 失败也冷却:避免每轮重试同一个报错
		return result, err
	}

	// 先按**落盘上限**截断,再做保全校验。
	// 必须这个顺序:校验的应当是真正写进去的那段文本 —— 否则会出现
	// "校验通过、截断后关键信息丢了"的假通过(保全校验就失去意义)。
	summaryText = pCommon.TruncateRunes(strings.TrimSpace(summaryText), summaryCap, hCommon.TruncatedMarker)
	if summaryText == pCommon.BlankString {
		GetCompactionThrottle().RecordFailure(session.SessionId, now) // 调用已发生,同样计入冷却
		return result, nil
	}

	// 关键信息保有率:**两种模式都计算**(纯本地字符串匹配,无额外调用),让 default 模式的
	// 信息丢失可观测;safeguard 模式下它还会驱动降级。
	ratio, missed := keyRetentionRatio(run, summaryText)

	// 【模式差异 2/3 + 3/3】保全校验与覆盖区间调整 —— **safeguard 专属**(default 恒为 [from,to])
	// 校验不过时不扩大覆盖区间:收窄到区间前半,较新的原文继续以原文回放(宁可多留,不可丢失)。
	if mode == hCommon.CompactionModeSafeguard { //safeguard
		if ratio < hCommon.SafeguardMinKeyRetention {
			result.Degraded = true
			coverTo = degradeCoverTo(run, coverFrom, coverTo)
			logging.LogWarnF("chat memory compact: mode=safeguard key retention %.2f below floor, degrade covers to %d-%02d (missed %v)",
				ratio, coverFrom, coverTo, missed)

			// 降级后按**新的覆盖区间**重算:降级到底保住了没有,必须可观测
			if degraded := recordsInRange(run, coverFrom, coverTo); len(degraded) > 0 {
				if degradedRatio, degradedMissed := keyRetentionRatio(degraded, summaryText); degradedRatio < hCommon.SafeguardMinKeyRetention {
					logging.LogWarnF("chat memory compact: session=%s degraded range still below floor (retention=%.2f missed=%v)",
						session.SessionId, degradedRatio, degradedMissed)
				}
			}
		}
	} else if ratio < hCommon.SafeguardMinKeyRetention {
		// default 模式不降级(它承诺整段替换),但必须留下痕迹:否则"摘要丢了关键信息"无从发现
		logging.LogWarnF("memory compact: session=%s mode=default low key retention %.2f (missed %v)",
			session.SessionId, ratio, missed)
	}

	// 落盘前把关:covers 区间必须精确等于送进模型的记录集合
	consumed := make(map[int]bool, len(run))
	for i := range run {
		consumed[run[i].Index] = true
	}
	covered := hCommon.CoveredIndexes(hCommon.SummariesOf(records))
	if err := verifyCovers(records, coverFrom, coverTo, consumed, covered); err != nil {
		logging.LogErrorF("memory compact: session=%s refuse to write summary: %s", session.SessionId, err.Error())
		GetCompactionThrottle().RecordFailure(session.SessionId, now)
		return result, err
	}

	content := buildSummaryRecordContent(coverFrom, coverTo, summaryText)
	index, err := GetMemoryFileWriter().Append(session.Sender, session.SessionId, fileName, hCommon.HistoryRecordTypeSummary, pCommon.BlankString,
		content, pCommon.BlankString, 0)
	if err != nil {
		GetCompactionThrottle().RecordFailure(session.SessionId, now)
		return result, err
	}

	GetCompactionThrottle().RecordSuccess(session.SessionId, now, result.Degraded)
	result.SummaryIndex, result.CoversFrom, result.CoversTo, result.Summary = index, coverFrom, coverTo, summaryText
	logging.LogInfoF("chat memory compact: session=%s summary_index=%d covers=%d-%d mode=%d(%s) degraded=%v retention=%.2f src_runes=%d summary_runes=%d %s",
		session.SessionId, index, coverFrom, coverTo, mode, hCommon.CompactionModeName(mode), result.Degraded,
		ratio, srcRunes, utf8.RuneCountInString(summaryText), hCommon.CompactionStat(run))
	return result, nil
}

// distillWatermark 读取 MEMORY.md 里本会话已蒸馏到的记录序号(0 = 还没蒸过)。
// ok=false 表示水位**未知**(读不到文件/工作区不存在),与"水位为 0"区分开:
// 前者不能用来做任何收窄决策,后者是明确的"全都没蒸过"。
//
// 调用点都在"已经决定要花一次 LLM 调用"的路径上(蒸馏压力检查、压缩判定),文件本身很小,
// 因此在请求路径上可接受。
func (s *MemoryService) distillWatermark(agent dataobject.TAgent, sessionId string) (int, bool) {
	if !s.fileStorageUsable() {
		return 0, false
	}
	path, ok := memoryDocPath(agent)
	if !ok {
		return 0, false
	}
	content, err := pCommon.ReadTextIfExists(path)
	if err != nil {
		return 0, false
	}
	return lastDistilledIndex(content, sessionId), true
}

// distillWithBudget 与 Distill 相同,但源文本预算(batch 的 rune 上限)由调用方给定。
//
// 增量语义:按 MEMORY.md 里该会话的**水位**(自动区注释的 watermarks=… 或旧格式小节的 records a-b)
// 只蒸馏水位之后的记录;并把"已有的长期记忆列表"一并送进模型,让它输出**更新后的完整列表** ——
// 于是自动记忆区体积天然有界(整体重写),不需要按"最旧优先"删旧事实。
//
// 返回:蒸馏结果;未执行(记录太少/无工作区/无新事实信号)时返回零值且 err 为 nil。
func (s *MemoryService) distillWithBudget(ctx context.Context, session dataobject.TSession, records []hCommon.MemoryRecord,
	agent dataobject.TAgent, summarize hCommon.Summarizer, budget int) (DistillResult, error) {
	var result DistillResult
	if summarize == nil || agent.AgentId == pCommon.BlankString {
		return result, nil
	}
	if !s.fileStorageUsable() {
		return result, nil
	}

	path, ok := memoryDocPath(agent)
	if !ok {
		logging.LogWarnF("chat memory distill: agent %s workspace not found, skip", agent.AgentId)
		return result, nil
	}

	// 待蒸馏集合:未被摘要代表的普通记录(被摘要覆盖的记录既不回放、也不该再蒸馏)
	active := hCommon.ActivePlainRecords(records)

	existing, readErr := pCommon.ReadTextIfExists(path)
	if readErr != nil {
		return result, readErr
	}
	watermark := lastDistilledIndex(existing, session.SessionId)
	pending := pendingRecords(active, watermark)

	// 首次蒸馏要求"记录足够多"(短会话没有长期价值);增量批次只要求非空 ——
	// 批次小是因为水位已经推进过,不是因为会话太短。
	need := 1
	if watermark <= 0 {
		need = hCommon.DistillMinRecords
	}
	if len(pending) < need {
		logging.LogDebugF("chat memory distill: session=%s skip, pending=%d watermark=%d need=%d",
			session.SessionId, len(pending), watermark, need)
		return result, nil
	}

	// 本地预筛:这批记录里没有任何"可能长期有效"的信号就直接跳过,不发起 LLM 调用。
	// (按轮次节拍无条件调用,会在"这 10 轮只是闲聊/查天气"时白花一次调用。)
	if !hasDurableSignal(pending) {
		// 回传**已判定**的区间:这批记录本地已判定"没有长期价值",调用方因此可以放心
		// 让压缩覆盖它们 —— 否则它们会永久卡住压缩(既不进长期记忆,也不让历史变小)。
		result.CoversFrom, result.CoversTo = pending[0].Index, pending[len(pending)-1].Index
		logging.LogDebugF("chat memory distill: session=%s skip, no durable signal in %d pending records (watermark=%d, judged %d-%d)",
			session.SessionId, len(pending), watermark, result.CoversFrom, result.CoversTo)
		return result, nil
	}

	// 送入预算内取**连续前缀**(溢出的留在队列里下一轮继续,不留永久空洞)
	batch, newLines, backlog := windowPrefix(pending, budget)
	if backlog > 0 {
		// 可见的"队列积压":说明单轮产出超过了送入预算,建议调大 DistillMaxInputChars 或调小间隔
		logging.LogWarnF("chat memory distill: session=%s batch is bounded by the input budget, %d records left for the next round",
			session.SessionId, backlog)
	}

	// 已有的长期记忆(自动区 + 迁移期的旧格式小节)一并送入:模型据此输出**更新后的完整列表**
	oldFacts, _ := existingMemoryFacts(strings.Split(existing, "\n"))
	prompt := buildDistillUserPrompt(oldFacts, batch, newLines)

	text, err := summarize(ctx, distillSystemPrompt, prompt)
	if err != nil {
		return result, err
	}

	facts := parseDistilledFacts(text)

	result.Path = path
	result.CoversFrom, result.CoversTo, result.Backlog = batch[0].Index, batch[len(batch)-1].Index, backlog

	// 模型判定"没有值得长期记住的内容":**只推进水位、保留已有记忆**(不能把旧记忆清空)。
	// 例外在写入端:文件还是旧格式遗留内容时,模型明确说 NONE 允许清空条目(见 appendFactsToMemoryDoc)。
	if len(facts) == 0 {
		if len(oldFacts) == 0 {
			// 还没有任何长期记忆:不必为"空"创建一个只有注释的文件(它会成为每次请求的噪声),
			// 也不要推进水位 —— 下一轮再判断一次即可。
			return result, nil
		}
		if written, skipped, updated, removed, migrated, err := appendFactsToMemoryDoc(memoryDocWrite{
			path: path, facts: oldFacts, sessionId: session.SessionId,
			fromIndex: result.CoversFrom, toIndex: result.CoversTo,
			maxChars: hCommon.DistillMemoryDocMaxChars,
			snapshot: oldFacts, explicitNone: isExplicitNone(text),
		}); err == nil {
			result.Facts, result.Skipped, result.Updated, result.Removed, result.Migrated =
				written, skipped, updated, removed, migrated
		}
		return result, nil
	}

	written, skipped, updated, removed, migrated, err := appendFactsToMemoryDoc(memoryDocWrite{
		path: path, facts: facts, sessionId: session.SessionId,
		fromIndex: result.CoversFrom, toIndex: result.CoversTo,
		maxChars: hCommon.DistillMemoryDocMaxChars,
		snapshot: oldFacts,
	})
	if err != nil {
		return result, err
	}
	result.Facts, result.Skipped, result.Updated, result.Removed, result.Migrated = written, skipped, updated, removed, migrated

	if len(written) > 0 || updated > 0 || removed > 0 || migrated > 0 {
		logging.LogInfoF("chat memory distill: agent=%s session=%s records=%d-%d facts=%d updated=%d removed=%d migrated=%d skipped=%d backlog=%d",
			agent.AgentId, session.SessionId, result.CoversFrom, result.CoversTo,
			len(written), updated, removed, migrated, skipped, backlog)
	}
	return result, nil
}

// mergeAttempt 用至多 maxSize 条最旧摘要做**一次**合并尝试(maxSize<=0 表示不限制)。
// 调用方必须已持压缩锁并通过节流闸;本函数只负责这一次尝试本身(err != nil 时 state 无意义)。
func (s *MemoryService) mergeAttempt(ctx context.Context, session dataobject.TSession,
	records []hCommon.MemoryRecord, fileName string, maxSize, budget int,
	summarize hCommon.Summarizer) (CompactionResult, mergeAttemptState, error) {
	result := CompactionResult{Mode: hCommon.CompactionModeDefault}

	set, from, to := mergeCandidateUpTo(records, maxSize)
	if len(set) == 0 {
		return result, mergeNoCandidate, nil
	}

	// 收益门槛(与 Compact 同一门槛):合并后不会变小就毫无意义 ——
	// 摘要下限 SummaryMinRunes 可能大于被合并摘要的总量,此时"合并"反而会把 system 撑大。
	srcRunes := hCommon.RecordsRunes(set)
	summaryCap := effectiveSummaryCap(srcRunes)
	if srcRunes <= summaryCap {
		logging.LogDebugF("chat memory merge: session=%s skipped, no gain (src_runes=%d <= summary_cap=%d)",
			session.SessionId, srcRunes, summaryCap)
		return result, mergeNoGain, nil
	}

	prompt := buildMergeUserPrompt(set, from, to)
	if utf8.RuneCountInString(prompt) > budget {
		// 不截断:截断会让"被取代的摘要"内容没送进模型,而 covers 已经承诺取代它们
		logging.LogWarnF("chat memory merge: session=%s merged set is too large to fit the input budget (%d runes), skip", session.SessionId, budget)
		return result, mergeTooLarge, nil
	}

	text, err := summarize(ctx, compactionMergeSystemPrompt, prompt)
	if err != nil {
		return result, mergeNoGain, err
	}

	text = pCommon.TruncateRunes(strings.TrimSpace(text), summaryCap, hCommon.TruncatedMarker)
	if text == pCommon.BlankString {
		logging.LogWarnF("chat memory merge: session=%s summarizer returned empty text (merged=%d)", session.SessionId, len(set))
		return result, mergeEmptyOutput, nil
	}

	// 保全校验:合并是**最激进**的一步(把 N 条摘要压成 1 条,去掉的绝对量最大),
	// 因此与 Compact 一样必须检查关键信息保有率。不达标宁可不写 ——
	// 摘要超预算只是多花 token,丢信息不可逆(而 covers 已经承诺取代那些摘要)。
	ratio, missed := keyRetentionRatio(set, text)
	if ratio < hCommon.SafeguardMinKeyRetention {
		logging.LogWarnF("chat memory merge: session=%s key retention %.2f below floor, refuse to write (merged=%d missed %v)",
			session.SessionId, ratio, len(set), missed)
		return result, mergeLossy, nil
	}

	consumed := make(map[int]bool, len(set))
	for i := range set {
		consumed[set[i].Index] = true
	}
	covered := hCommon.CoveredIndexes(hCommon.SummariesOf(records))
	if err := verifyCovers(records, from, to, consumed, covered); err != nil {
		logging.LogErrorF("chat memory merge: session=%s refuse to write merged summary: %s", session.SessionId, err.Error())
		return result, mergeNoCandidate, err
	}

	content := buildSummaryRecordContent(from, to, text)
	index, err := GetMemoryFileWriter().Append(session.Sender, session.SessionId, fileName, hCommon.HistoryRecordTypeSummary, pCommon.BlankString,
		content, pCommon.BlankString, 0)
	if err != nil {
		return result, mergeNoCandidate, err
	}

	result.SummaryIndex, result.CoversFrom, result.CoversTo, result.Summary = index, from, to, text
	result.Merged = len(set)
	logging.LogInfoF("chat memory merge: session=%s summary_index=%d covers=%d-%d merged=%d retention=%.2f src_runes=%d summary_runes=%d",
		session.SessionId, index, from, to, result.Merged, ratio, srcRunes, utf8.RuneCountInString(text))
	return result, mergeWritten, nil
}

// mergeWithBudget 与 CompactSummaries 相同,但源文本预算由调用方给定(见 ctxmgr.MaintenanceBudgets)。
func (s *MemoryService) mergeWithBudget(ctx context.Context, session dataobject.TSession,
	records []hCommon.MemoryRecord, summarize hCommon.Summarizer, budget int) (CompactionResult, error) {
	result := CompactionResult{Mode: hCommon.CompactionModeDefault}
	if !s.fileStorageUsable() || summarize == nil {
		return result, nil
	}

	fileName := strings.TrimSpace(session.ChatData)
	if fileName == pCommon.BlankString {
		return result, nil
	}

	unlock := GetStorageLocks().Lock(hCommon.BuildCompactionLockKey(session.SessionId))
	defer unlock()

	// 节流(与压缩同一张表、不同键):
	// 合并的触发条件(摘要超预算)一旦成立就是**持续为真**,而它的失败(ex. 无可安全合并的集合、
	// 合并后关键信息丢太多)也是持续状态 —— 没有冷却就会每轮重来一次(LLM 调用或全量读回)。
	// 位置在"重读 + 挑选集合"之前:冷却期内连判断都不做。
	now := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	mergeKey := mergeThrottleKey(session.SessionId)
	if !GetCompactionThrottle().Allow(mergeKey, now) {
		logging.LogDebugF("chat memory merge: session=%s in cooldown, skip attempt", session.SessionId)
		return result, nil
	}

	records = s.reloadForCompaction(session, fileName, records)

	// 尝试顺序见 mergeAttemptSizes:先"一次尽量多合并",保留率不达标再退回最小集合。
	sizes := mergeAttemptSizes(len(hCommon.SummariesOf(records)))

	var last CompactionResult
	lastState := mergeNoCandidate
	for i, maxSize := range sizes {
		attempt, state, err := s.mergeAttempt(ctx, session, records, fileName, maxSize, budget, summarize)
		if err != nil {
			GetCompactionThrottle().RecordFailure(mergeKey, now) // 失败也冷却:避免每轮重试同一个报错
			return attempt, err
		}
		if state == mergeWritten {
			GetCompactionThrottle().RecordSuccess(mergeKey, now, false)
			return attempt, nil
		}

		last, lastState = attempt, state
		if state == mergeNoCandidate {
			break // 任何规模都挑不出安全集合:退一步也同样挑不出(要换的是内容,不是规模)
		}
		if i+1 < len(sizes) {
			logging.LogDebugF("chat memory merge: session=%s attempt (up to %d summaries) made no progress (%s), retry with the oldest %d summaries",
				session.SessionId, maxSize, state, hCommon.CompactionMergeMinSummaries)
		}
	}

	// 本轮没有取得进展:进入冷却(否则每轮都会重来一遍)。
	GetCompactionThrottle().RecordFailure(mergeKey, now)
	switch lastState {
	case mergeNoCandidate:
		// 没有可安全合并的集合 = 摘要将持续超预算(注入体积只增不减,历史预算随之变小),
		// 这是唯一信号,不能用 debug 级别埋掉。
		logging.LogWarnF("chat memory merge: session=%s summaries over budget but no safely mergeable set (summaries=%d runes=%d)",
			session.SessionId, len(hCommon.SummariesOf(records)), hCommon.SummariesRunes(records))
	case mergeLossy:
		// 连最小集合都保不住关键标记:摘要会长期超预算、压缩被禁用,
		// 此后掉出窗口的记录将没有任何摘要代表 —— 必须人工介入(整理摘要/调大窗口)。
		logging.LogWarnF("chat memory merge: session=%s no lossless merge at any candidate size (summaries=%d runes=%d): summaries stay over budget and compaction stays disabled, check the summary content",
			session.SessionId, len(hCommon.SummariesOf(records)), hCommon.SummariesRunes(records))
	}
	return last, nil
}

// ==================== non-classic private functions & methods ===========================
// buildSummaryByMode 生成摘要文本
//
//	default   (CompactionModeDefault  , 0):整段区间一次调用;
//	safeguard (CompactionModeSafeguard, 1):区间超过 safeguardBlockRecords 时分块多次调用后拼接。
func buildSummaryByMode(ctx context.Context, targets []hCommon.MemoryRecord, from, to, mode int, summarize hCommon.Summarizer) (string, error) {
	switch mode {
	case hCommon.CompactionModeSafeguard: //safeguard
		return summarizeInBlocks(ctx, targets, summarize)
	default: // default compaction mode
		return summarizeOnce(ctx, targets, from, to, summarize)
	}
}

// distillPressureAt 按水位统计待蒸馏量(纯函数:让请求路径"一次读文件、两处使用")。
//
// 体积口径与**送批**完全一致(逐条按 promptLineRunes 计):一个是"数出来的",一个是"渲染出来的",
// 但两者必须同值,否则会出现"压力说攒够一轮了"而实际批次只装了零头(长工具记录),
// 或反过来(多条短记录)在批次早已满时还不触发。一致性由 TestPromptLineRunesMatchesRendered 锁住。
func distillPressureAt(watermark int, records []hCommon.MemoryRecord) (pendingRecords, pendingChars int) {
	for i := range records {
		if records[i].Index <= watermark || records[i].IsSummary() {
			continue
		}
		pendingRecords++
		pendingChars += promptLineRunes(records[i])
		if pendingChars >= hCommon.DistillMaxInputChars {
			break // 够一轮就够判断了
		}
	}
	return pendingRecords, pendingChars
}
