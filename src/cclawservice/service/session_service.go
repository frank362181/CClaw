/**
* 会话管理服务(阶段1:双写落库 + 历史文件)
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

package service

import (
	"strings"
	"sync"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/harness/ctxmgr"
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

// turnRecorder 收集"本轮调用过的工具"。
// 必须并发安全:流式路径同一轮的多个工具是并行执行的。
type turnRecorder struct {
	mu    sync.Mutex
	tools []TurnToolRecord
}

func newTurnRecorder() *turnRecorder {
	return &turnRecorder{}
}

// Record 登记一次工具调用(rec 为 nil 时静默忽略,便于同步/流式共用同一份工具循环代码)。
func (r *turnRecorder) Record(toolName string, callTime, respTime int64, result string) {
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
func (r *turnRecorder) Snapshot() []TurnToolRecord {
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

// ChatSessionService 会话管理服务。
type ChatSessionService struct {
	baseService.BaseService

	sessionAccessor dataaccessor.ISessionAccessor
	enabled         bool
}

var chatSessionService = pCommon.GetSingleton(func() *ChatSessionService {
	return &ChatSessionService{
		BaseService: baseService.BaseService{
			Name:        "chatSessionService",
			Description: "persist chat sessions, turns and tool calls (phase 1: write-only side channel)",
		},
	}
}).Instance()

// GetChatSessionService 构建会话管理服务。
//   - enabled:来自 ini [chat] sessionRecord,关闭时本服务完全旁路(不写库、不写文件);
//   - 数据访问器构建失败时自动降级为"关闭",保证聊天主流程不受影响。
func GetChatSessionService(db core.IBaseDatabase, enabled bool) *ChatSessionService {
	accessor, err := dataaccessor.GetSessionAccessor(db)
	if err != nil {
		logging.LogErrorF("chat session service: build accessor failed, session record disabled: %s", err.Error())
		chatSessionService.sessionAccessor = nil
		chatSessionService.enabled = false
		return chatSessionService
	}

	chatSessionService.sessionAccessor = accessor
	chatSessionService.enabled = enabled
	return chatSessionService
}

// Enabled 报告本服务是否生效(供调用方跳过无谓的记录开销)。
func (s *ChatSessionService) Enabled() bool {
	return s.usable()
}

// Begin 会话建档(幂等):不存在则创建 t_session + 历史文件,已存在则复用并按需补齐历史文件名。
// 返回值 ok=false 表示记录不可用(未启用/出错),调用方应继续聊天流程。
func (s *ChatSessionService) Begin(sender string, agent dataobject.TAgent, req *request.ChatRequest) (dataobject.TSession, bool) {
	if !s.usable() || req == nil {
		return dataobject.TSession{}, false
	}

	sessionId := cCommon.NormalizeCompactId(req.SessionId)
	if sessionId == "" {
		logging.LogWarnF("chat session service: empty session id, skip record")
		return dataobject.TSession{}, false
	}

	unlock := ctxmgr.GetSessionLocks().Lock(sessionId)
	defer unlock()

	now := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime()

	// 1. 会话已存在 → 复用(常见路径:会话的第 2..n 条消息)
	existing, err := s.sessionAccessor.GetSession(sessionId)
	if err != nil {
		logging.LogWarnF("chat session service: load session %s error:%s", sessionId, err.Error())
		return dataobject.TSession{}, false
	}
	if existing != nil {
		// 归属校验:sessionId 由客户端提供,而 t_session 按 Excel 设计没有 owner 列,
		// sender 是唯一可用的归属凭据。若不一致,说明客户端在复用他人会话 ——
		// 必须拒绝记录,否则会把 B 的问答追加进 A 的会话与历史文件。
		if !strings.EqualFold(strings.TrimSpace(existing.Sender), strings.TrimSpace(sender)) {
			logging.LogWarnF("chat session service: session %s belongs to another sender, skip record", sessionId)
			return dataobject.TSession{}, false
		}
		return s.ensureHistoryFile(sessionId, *existing, now), true
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
	fileName, fileErr := ctxmgr.GetHistoryFileWriter().Ensure(sessionId, now, s.historyHeaderLines(session, now))
	if fileErr != nil {
		// 文件失败不阻断建档:chat_data 留空,后续轮次会再尝试补齐
		logging.LogWarnF("chat session service: create history file for %s error:%s", sessionId, fileErr.Error())
	} else {
		session.ChatData = fileName
	}

	if err = s.sessionAccessor.CreateSession(&session); err != nil {
		// 并发下可能已被其它请求建档(唯一键冲突):回读一次,复用先到者
		if again, loadErr := s.sessionAccessor.GetSession(sessionId); loadErr == nil && again != nil {
			return s.ensureHistoryFile(sessionId, *again, now), true
		}
		logging.LogWarnF("chat session service: create session %s error:%s", sessionId, err.Error())
		return dataobject.TSession{}, false
	}

	return session, true
}

// AppendTurn 记录一轮:写入 t_turn + t_turn_tool,并把内容追加到历史文件。
// reply 为最终回复文本,tools 为本轮调用过的工具(可为空)。
func (s *ChatSessionService) AppendTurn(sess dataobject.TSession, req *request.ChatRequest, reply string, tools []TurnToolRecord) {
	if !s.usable() || req == nil {
		return
	}

	sessionId := cCommon.NormalizeCompactId(sess.SessionId)
	if sessionId == "" {
		return
	}

	unlock := ctxmgr.GetSessionLocks().Lock(sessionId)
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
		return
	}

	// 3. 分配会话内轮次序号(持锁下"读最大值 + 1")
	maxSeq, err := s.sessionAccessor.GetMaxTurnSeq(sessionId)
	if err != nil {
		logging.LogWarnF("chat session service: max turn seq of %s error:%s", sessionId, err.Error())
		return
	}

	// 4. 分配历史记录序号:权威来源是 DB(t_turn.resp_index 的最大值 + 1)
	maxRespIndex, err := s.sessionAccessor.GetMaxRespIndex(sessionId)
	if err != nil {
		logging.LogWarnF("chat session service: max resp index of %s error:%s", sessionId, err.Error())
		return
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
	writer := ctxmgr.GetHistoryFileWriter()

	// 5. 历史文件:按真实发生顺序写入 问题 → 工具 → 回复。
	//    每条都带上期望序号(从 DB 推出的 nextIndex 起递增),
	//    写入器会取"DB 序号与本进程缓存"的最大值,保证序号只增不减(见写入器的序号契约)。
	chatIndex := 0
	if fileName != "" {
		if idx, err := writer.Append(sessionId, fileName, ctxmgr.HistoryRecordUser, "", req.ReqMsg, pCommon.TimestampText(chatTime), nextIndex); err == nil {
			chatIndex = idx
		} else {
			logging.LogWarnF("chat session service: append user record error:%s", err.Error())
		}
	}

	toolRows := make([]dataobject.TTurnTool, 0, len(tools))
	for i, tool := range tools {
		toolRows = append(toolRows, dataobject.TTurnTool{
			Seq:        i + 1,
			ToolCalled: true,
			CallTime:   tool.CallTime,
			ToolName:   tool.ToolName,
			ToolResp:   pCommon.TruncateRunes(strings.TrimSpace(tool.Result), cCommon.TurnToolRespMaxLen),
			RespTime:   tool.RespTime,
		})
		if fileName != "" {
			if _, err := writer.Append(sessionId, fileName, ctxmgr.HistoryRecordTool, tool.ToolName, tool.Result, pCommon.TimestampText(tool.CallTime), nextIndex+1+i); err != nil {
				logging.LogWarnF("chat session service: append tool record error:%s", err.Error())
			}
		}
	}

	respIndex := 0
	if fileName != "" {
		if idx, err := writer.Append(sessionId, fileName, ctxmgr.HistoryRecordAssistant, "", reply, pCommon.TimestampText(now.Timestamp), nextIndex+1+len(tools)); err == nil {
			respIndex = idx
		} else {
			logging.LogWarnF("chat session service: append assistant record error:%s", err.Error())
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
		return
	}

	// 7. 落库轮内工具
	for i := range toolRows {
		toolRows[i].TurnId = turn.TurnId
	}
	if err = s.sessionAccessor.CreateTurnTools(toolRows); err != nil {
		logging.LogWarnF("chat session service: create turn tools of %s error:%s", turn.TurnId, err.Error())
	}
}

// EndSession 结束会话(供后续阶段:空闲超时/手动结束/触发 compact 调用)。
// 阶段1 不主动调用:结束语义属于会话生命周期管理,留待接入空闲超时与压缩时统一编排。
func (s *ChatSessionService) EndSession(sessionId string, reason int) error {
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

// usable 报告记录能力是否可用(已启用 + 数据访问器就绪)。
func (s *ChatSessionService) usable() bool {
	return s != nil && s.enabled && s.sessionAccessor != nil
}

// ensureHistoryFile 保证会话已有历史文件:chat_data 为空(此前创建失败)时补建并回写。
func (s *ChatSessionService) ensureHistoryFile(sessionId string, session dataobject.TSession, now *pCommon.DatetimeItem) dataobject.TSession {
	if strings.TrimSpace(session.ChatData) != "" {
		return session
	}

	fileName, err := ctxmgr.GetHistoryFileWriter().Ensure(sessionId, now, s.historyHeaderLines(session, now))
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

// historyHeaderLines 构造历史文件头部的元数据行。
func (s *ChatSessionService) historyHeaderLines(session dataobject.TSession, now *pCommon.DatetimeItem) []string {
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
