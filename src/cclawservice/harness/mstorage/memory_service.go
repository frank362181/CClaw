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
* 阶段1 的定位是"只写不读":
*   - 不参与 system prompt 组装、不参与上下文装配,因此对现有聊天行为零影响;
*   - 所有步骤失败仅记日志并吞掉(旁路),绝不因为记录失败而影响给用户的回复。
*
* 并发:会话内 seq(轮次序号)与历史文件 index(记录序号)都是"读最大值 + 1"的分配,
* 必须串行,统一由 ctxmgr.GetSessionLocks() 按 sessionId 加锁保护。
**/

package mstorage

import (
	"context"
	"strings"
	"sync"
	"unicode/utf8"

	cCommon "my.favorite/cclawservice/common"
	hCommon "my.favorite/cclawservice/harness/common"
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
	RespTime int64  // 返回时间(Unix 秒)
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

// Record 登记一次工具调用(rec 为 nil 时静默忽略,便于同步/流式共用同一份工具循环代码)。
func (r *TurnRecorder) Record(toolName string, callTime, respTime int64, result string) {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools = append(r.tools, TurnToolRecord{
		ToolName: strings.TrimSpace(toolName),
		Result:   result,
		CallTime: callTime,
		RespTime: respTime,
	})
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
		logging.LogErrorF("chat session service: build accessor failed, session record disabled: %s", err.Error())
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

// Begin 会话建档(幂等):不存在则创建 t_session + 历史文件,已存在则复用并按需补齐历史文件名。
// 返回值 ok=false 表示记录不可用(未启用/出错),调用方应继续聊天流程。
func (s *MemoryService) Begin(sender string, agent dataobject.TAgent, req *request.ChatRequest) (dataobject.TSession, bool) {
	if !s.usable() || req == nil {
		return dataobject.TSession{}, false
	}

	sessionId := cCommon.NormalizeCompactId(req.SessionId)
	if sessionId == "" {
		logging.LogWarnF("chat session service: empty session id, skip record")
		return dataobject.TSession{}, false
	}

	unlock := GetStorageLocks().Lock(sessionId)
	defer unlock()

	now := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime()

	// 1. 会话已存在 → 复用(常见路径:会话的第 2..n 条消息)
	existing, err := s.sessionAccessor.GetSession(sessionId)
	if err != nil {
		logging.LogWarnF("chat session service: load session %s error:%s", sessionId, err.Error())
		return dataobject.TSession{}, false
	}
	if existing != nil {
		// sender 是唯一可用的归属凭据。若不一致,说明客户端在复用他人会话 ——
		// 必须拒绝记录,否则会把 B 的问答追加进 A 的会话与历史文件。
		if _, ok := s.isSameOwner(sender, existing); !ok {
			return dataobject.TSession{}, false
		}
		return s.ensureMemoryFile(sessionId, *existing, now), true
	}

	// 2. 首条消息 → 建档
	session := dataobject.TSession{
		SessionId:      sessionId,
		SessionName:    cCommon.BuildSessionName(req.ReqMsg),
		AgentId:        strings.TrimSpace(agent.AgentId),
		BeginTimestamp: now.Timestamp,
		ChatType:       req.ChatType,
		ChatChannel:    req.ChatChannel,
		Sender:         strings.TrimSpace(sender),
	}

	// 先落历史文件:文件名要一并写进 t_session.chat_data
	fileName, fileErr := GetMemoryFileWriter().Ensure(sessionId, now, s.memoryFileHeaderLines(session, now))
	if fileErr != nil {
		// 文件失败不阻断建档:chat_data 留空,后续轮次会再尝试补齐
		logging.LogWarnF("chat session service: create history file for %s error:%s", sessionId, fileErr.Error())
	} else {
		session.ChatData = fileName
	}

	if err = s.sessionAccessor.CreateSession(&session); err != nil {
		// 并发下可能已被其它请求建档(唯一键冲突):回读一次,复用先到者
		if again, loadErr := s.sessionAccessor.GetSession(sessionId); loadErr == nil && again != nil {
			return s.ensureMemoryFile(sessionId, *again, now), true
		}
		logging.LogWarnF("chat session service: create session %s error:%s", sessionId, err.Error())
		return dataobject.TSession{}, false
	}

	return session, true
}

// AppendTurn 记录一轮:写入 t_turn + t_turn_tool,并把内容追加到历史文件。
// reply 为最终回复文本,tools 为本轮调用过的工具(可为空)。
//
// 返回本轮分配的会话内轮次序号(实际记下的序号;未记录/失败返回 0)。
// 调用方(记忆维护)据此判断是否需要蒸馏 —— 避免再跑一次 SELECT MAX(seq)。
func (s *MemoryService) AppendTurn(sess dataobject.TSession, req *request.ChatRequest, reply string, tools []TurnToolRecord) int {
	return s.appendTurn(sess, req, reply, tools, false)
}

// AppendInterruptedTurn 记录一轮**被中断**的轮次(用户点中断/客户端断开/任务超时)。
//
// 为什么必须记录:
//   - 用户已经看到了（部分）回复,历史却不记,下一轮读回时会出现"问题在、回答不在"的断裂,
//     模型会把用户随后的追问当成无源之问;
//   - 该轮调用过的工具已经产生副作用,不记就无法审计。
//
// 表示方式(不新增列/表):
//   - 历史文件中助手记录头标记为 assistant:interrupted,正文为中断前已产出的部分;
//   - t_turn.resp_index 指向该记录(内容为空时不写助手记录,resp_index = 0)。
//
// reason 仅用于日志(如 context canceled / run timeout),不落库。
// 返回本轮分配的轮次序号(未记录返回 0)。
func (s *MemoryService) AppendInterruptedTurn(sess dataobject.TSession, req *request.ChatRequest, partial string, tools []TurnToolRecord, reason string) int {
	seq := s.appendTurn(sess, req, partial, tools, true)
	if strings.TrimSpace(reason) != pCommon.BlankString {
		logging.LogInfoF("chat session service: interrupted turn recorded, session=%s turn=%s reason=%s partial_chars=%d",
			sess.SessionId, req.TurnId, reason, len([]rune(partial)))
	}
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
	return GetMemoryFileWriter().Read(session.SessionId, fileName)
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

// Compact 把 [from, to] 区间内的记录压缩为一条摘要记录。
//
//   - session:   所属会话(chat_data 即历史文件名)
//   - records:   调用方读回的快照(会在持锁后重新读一次,见 reloadForCompaction)
//   - from/to:   待压缩的记录序号闭区间(来自装配器报告的"被裁掉的区间")
//   - mode:      dataobject.CompactionModeDefault / CompactionModeSafeguard
//   - summarize: 摘要生成器(注入 LLM 调用)
//
// 返回值:压缩结果;未执行压缩时 SummaryIndex 为 0 且 err 为 nil(属正常情况,非错误)。
//
// P0 契约:covers 区间必须**精确等于**送进模型的记录集合。
// 读取端会无条件信任 covers(让区间内记录永久退出回放),所以这里三重把关:
// 区间连续且不含摘要记录(compactionRun)→ 放不下时收缩区间而非丢弃记录(fitCompactionRange)
// → 落盘前再校验一次(verifyCovers)。
func (s *MemoryService) Compact(ctx context.Context, session dataobject.TSession, records []hCommon.MemoryRecord,
	from, to, mode int, summarize Summarizer) (CompactionResult, error) {
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
	run, fit := fitCompactionRange(run)
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
	summaryText, err := s.buildSummaryByMode(ctx, run, coverFrom, coverTo, mode, summarize)
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
		logging.LogWarnF("chat memory compact: session=%s mode=default low key retention %.2f (missed %v)",
			session.SessionId, ratio, missed)
	}

	// 落盘前把关:covers 区间必须精确等于送进模型的记录集合
	consumed := make(map[int]bool, len(run))
	for i := range run {
		consumed[run[i].Index] = true
	}
	covered := hCommon.CoveredIndexes(hCommon.SummariesOf(records))
	if err := verifyCovers(records, coverFrom, coverTo, consumed, covered); err != nil {
		logging.LogErrorF("chat memory compact: session=%s refuse to write summary: %s", session.SessionId, err.Error())
		GetCompactionThrottle().RecordFailure(session.SessionId, now)
		return result, err
	}

	content := buildSummaryRecordContent(coverFrom, coverTo, summaryText)
	index, err := GetMemoryFileWriter().Append(session.SessionId, fileName, hCommon.HistoryRecordTypeSummary, pCommon.BlankString,
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

// CompactSummaries 把最旧的一批摘要合并为一条摘要(B2:摘要总量超预算时调用)。
//
// 与 Compact 的区别:输入是**摘要正文**而不是原始记录。合并后的 covers 区间刻意包含
// 被合并摘要的记录序号,这样读取端会把它们判定为"已被取代",从而真正缩小注入体积。
//
// 注意:合并**不区分 compaction_mode**(它是摘要自身的整理,不是"怎么摘要原始记录"),
// 因此结果里 Mode 恒为 default、Degraded 恒为 false;日志与结果里的 Merged>0 是唯一标识。
//
// 返回 SummaryIndex=0 表示没有可安全合并的集合(宁可超预算,也不写过度承诺的 covers)。
func (s *MemoryService) CompactSummaries(ctx context.Context, session dataobject.TSession,
	records []hCommon.MemoryRecord, summarize Summarizer) (CompactionResult, error) {
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

	records = s.reloadForCompaction(session, fileName, records)

	set, from, to := mergeCandidate(records)
	if len(set) == 0 {
		// 没有可安全合并的集合 = 摘要将持续超预算(注入体积只增不减,历史预算随之变小),
		// 这是唯一信号,不能用 debug 级别埋掉。
		logging.LogWarnF("chat memory merge: session=%s summaries over budget but no safely mergeable set (summaries=%d runes=%d)",
			session.SessionId, len(hCommon.SummariesOf(records)), hCommon.SummariesRunes(records))
		return result, nil
	}

	// 收益门槛(与 Compact 同一门槛):合并后不会变小就毫无意义 ——
	// 摘要下限 SummaryMinRunes 可能大于被合并摘要的总量,此时"合并"反而会把 system 撑大。
	srcRunes := hCommon.RecordsRunes(set)
	summaryCap := effectiveSummaryCap(srcRunes)
	if srcRunes <= summaryCap {
		logging.LogDebugF("chat memory merge: session=%s skipped, no gain (src_runes=%d <= summary_cap=%d)",
			session.SessionId, srcRunes, summaryCap)
		return result, nil
	}

	prompt := buildMergeUserPrompt(set, from, to)
	if utf8.RuneCountInString(prompt) > hCommon.MaxCompactionInputChars {
		// 不截断:截断会让"被取代的摘要"内容没送进模型,而 covers 已经承诺取代它们
		logging.LogWarnF("chat memory merge: session=%s merged set is too large to fit the input budget, skip", session.SessionId)
		return result, nil
	}

	text, err := summarize(ctx, compactionMergeSystemPrompt, prompt)
	if err != nil {
		return result, err
	}

	text = pCommon.TruncateRunes(strings.TrimSpace(text), summaryCap, hCommon.TruncatedMarker)
	if text == pCommon.BlankString {
		return result, nil
	}

	// 保全校验:合并是**最激进**的一步(把 N 条摘要压成 1 条,去掉的绝对量最大),
	// 因此与 Compact 一样必须检查关键信息保有率。不达标宁可不写 ——
	// 摘要超预算只是多花 token,丢信息不可逆(而 covers 已经承诺取代那些摘要)。
	ratio, missed := keyRetentionRatio(set, text)
	if ratio < hCommon.SafeguardMinKeyRetention {
		logging.LogWarnF("chat memory merge: session=%s key retention %.2f below floor, refuse to write (missed %v)",
			session.SessionId, ratio, missed)
		return result, nil
	}

	consumed := make(map[int]bool, len(set))
	for i := range set {
		consumed[set[i].Index] = true
	}
	covered := hCommon.CoveredIndexes(hCommon.SummariesOf(records))
	if err := verifyCovers(records, from, to, consumed, covered); err != nil {
		logging.LogErrorF("chat memory merge: session=%s refuse to write merged summary: %s", session.SessionId, err.Error())
		return result, err
	}

	content := buildSummaryRecordContent(from, to, text)
	index, err := GetMemoryFileWriter().Append(session.SessionId, fileName, hCommon.HistoryRecordTypeSummary, pCommon.BlankString,
		content, pCommon.BlankString, 0)
	if err != nil {
		return result, err
	}

	result.SummaryIndex, result.CoversFrom, result.CoversTo, result.Summary = index, from, to, text
	result.Merged = len(set)
	logging.LogInfoF("chat memory merge: session=%s summary_index=%d covers=%d-%d merged=%d retention=%.2f src_runes=%d summary_runes=%d",
		session.SessionId, index, from, to, result.Merged, ratio, srcRunes, utf8.RuneCountInString(text))
	return result, nil
}

// reloadForCompaction 在**持锁之后**重新读回记录(失败则退回调用方传入的快照)。
//
// 为什么要重读:调用方的快照是"读锁之前"的,看不到等锁期间别的协程已经追加的摘要,
// 用它做"区间是否已被压缩"的判定会对同一区间写出重复摘要(两条摘要互相不构成取代,内容会重复注入)。
// Read 有增量缓存(头指纹 + parsedOffset),文件只追加时只解析新增字节,代价很低。
func (s *MemoryService) reloadForCompaction(session dataobject.TSession, fileName string,
	fallback []hCommon.MemoryRecord) []hCommon.MemoryRecord {
	latest, err := GetMemoryFileWriter().Read(session.SessionId, fileName)
	if err != nil {
		logging.LogDebugF("chat memory compact: reload records of %s error:%s (fallback to caller snapshot)",
			session.SessionId, err.Error())
		return fallback
	}
	return latest
}

// Distill 把会话记录蒸馏为长期记忆并写入 Agent 工作区的 MEMORY.md。
//
// 增量语义:按 MEMORY.md 里该会话的**水位**(自动区注释的 watermarks=… 或旧格式小节的 records a-b)
// 只蒸馏水位之后的记录;并把"已有的长期记忆列表"一并送进模型,让它输出**更新后的完整列表** ——
// 于是自动记忆区体积天然有界(整体重写),不需要按"最旧优先"删旧事实。
//
// 返回:蒸馏结果;未执行(记录太少/无工作区/无新事实信号)时返回零值且 err 为 nil。
func (s *MemoryService) Distill(ctx context.Context, session dataobject.TSession, records []hCommon.MemoryRecord,
	agent dataobject.TAgent, summarize Summarizer) (DistillResult, error) {
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
		logging.LogDebugF("chat memory distill: session=%s skip, no durable signal in %d pending records (watermark=%d)",
			session.SessionId, len(pending), watermark)
		return result, nil
	}

	// 送入预算内取**连续前缀**(溢出的留在队列里下一轮继续,不留永久空洞)
	batch, newLines, backlog := windowPrefix(pending, hCommon.DistillMaxInputChars)
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

// ============================== private functions & methods ==============================

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

// 归属校验:sessionId 由客户端提供,而 t_session 按 Excel 设计没有 owner 列
func (s *MemoryService) isSameOwner(sender string, session *dataobject.TSession) (dataobject.TSession, bool) {
	if strings.EqualFold(strings.TrimSpace(sender), strings.TrimSpace(session.Sender)) {
		return *session, true
	}

	logging.LogWarnF("chat session service: session %s belongs to another sender, skip record", session.SessionId)
	return dataobject.TSession{}, false
}

// ensureMemoryFile 保证会话已有历史文件:chat_data 为空(此前创建失败)时补建并回写。
func (s *MemoryService) ensureMemoryFile(sessionId string, session dataobject.TSession, now *pCommon.DatetimeItem) dataobject.TSession {
	if strings.TrimSpace(session.ChatData) != "" {
		return session
	}

	fileName, err := GetMemoryFileWriter().Ensure(sessionId, now, s.memoryFileHeaderLines(session, now))
	if err != nil {
		logging.LogWarnF("chat session service: ensure history file for %s error:%s", sessionId, err.Error())
		return session
	}

	if err = s.sessionAccessor.SetSessionChatData(sessionId, fileName); err != nil {
		logging.LogWarnF("chat session service: set chat_data of %s error:%s", sessionId, err.Error())
	}
	session.ChatData = fileName
	return session
}

// appendTurn 轮次记录的公共实现;interrupted=true 时助手记录带中断标记。
// 返回本轮落库的会话内轮次序号(未记录/失败返回 0)。
func (s *MemoryService) appendTurn(sess dataobject.TSession, req *request.ChatRequest, reply string, tools []TurnToolRecord, interrupted bool) int {
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
		logging.LogWarnF("chat session service: duplicated turn %s of session %s, skip", turnId, sessionId)
		return 0
	}

	// 3. 分配会话内轮次序号(持锁下"读最大值 + 1")
	maxSeq, err := s.sessionAccessor.GetMaxTurnSeq(sessionId)
	if err != nil {
		logging.LogWarnF("chat session service: max turn seq of %s error:%s", sessionId, err.Error())
		return 0
	}

	// 4. 分配历史记录序号:权威来源是 DB(t_turn.resp_index 的最大值 + 1)
	maxRespIndex, err := s.sessionAccessor.GetMaxRespIndex(sessionId)
	if err != nil {
		logging.LogWarnF("chat session service: max resp index of %s error:%s", sessionId, err.Error())
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
		if idx, err := writer.Append(sessionId, fileName, hCommon.HistoryRecordTypeUser, "", req.ReqMsg, pCommon.TimestampText(chatTime), nextIndex); err == nil {
			chatIndex = idx
		} else {
			logging.LogWarnF("memory session service: append user record error:%s", err.Error())
		}
	}

	toolRows := make([]dataobject.TTurnTool, 0, len(tools))
	for i, tool := range tools {
		toolRows = append(toolRows, dataobject.TTurnTool{
			Seq:        i + 1,
			ToolCalled: true,
			CallTime:   tool.CallTime,
			ToolName:   tool.ToolName,
			ToolResp:   pCommon.TruncateRunes(strings.TrimSpace(tool.Result), hCommon.TurnToolRespMaxLen, pCommon.BlankString),
			RespTime:   tool.RespTime,
		})
		if fileName != "" {
			if _, err := writer.Append(sessionId, fileName, hCommon.HistoryRecordTypeTool, tool.ToolName, tool.Result, pCommon.TimestampText(tool.CallTime), nextIndex+1+i); err != nil {
				logging.LogWarnF("memory session service: append tool record error:%s", err.Error())
			}
		}
	}

	respIndex := 0
	assistantTitle := pCommon.BlankString
	if interrupted {
		assistantTitle = hCommon.HistoryRecordTypeInterrupted
	}
	// 中断且没有任何产出时不写助手记录:空记录无信息量,徒增噪声(resp_index 保持 0)
	if fileName != "" && (!interrupted || strings.TrimSpace(reply) != pCommon.BlankString) {
		if idx, err := writer.Append(sessionId, fileName, hCommon.HistoryRecordTypeAssistant, assistantTitle, reply, pCommon.TimestampText(now.Timestamp), nextIndex+1+len(tools)); err == nil {
			respIndex = idx
		} else {
			logging.LogWarnF("memory session service: append assistant record error:%s", err.Error())
		}
	}

	// 6. 落库轮次(turn_id 用客户端传入值,t_turn.turn_id 唯一约束作为最后一道幂等防线)
	turn := dataobject.TTurn{
		TurnId:    turnId,
		SessionId: sessionId,
		Seq:       maxSeq + 1,
		ChatTime:  chatTime,
		ChatIndex: chatIndex,
		RespTime:  now.Timestamp,
		RespIndex: respIndex,
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

// buildSummaryByMode 生成摘要文本
//
//	default   (CompactionModeDefault  , 0):整段区间一次调用;
//	safeguard (CompactionModeSafeguard, 1):区间超过 safeguardBlockRecords 时分块多次调用后拼接。
func (s *MemoryService) buildSummaryByMode(ctx context.Context, targets []hCommon.MemoryRecord, from, to, mode int, summarize Summarizer) (string, error) {
	switch mode {
	case hCommon.CompactionModeSafeguard: //safeguard
		return summarizeInBlocks(ctx, targets, summarize)
	default: // default compaction mode
		return summarizeOnce(ctx, targets, from, to, summarize)
	}
}
