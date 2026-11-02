package chat

import (
	"strings"
	"unicode/utf8"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/common/llmproto"
	"my.favorite/cclawservice/harness/ctxmgr"
	"my.favorite/cclawservice/harness/observatory"
	"my.favorite/cclawservice/harness/toolexecutor"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/request"
	pCommon "my.favorite/platformutility/common"
	pException "my.favorite/platformutility/exception"
)

func NewCommand(comCode, sender string, chatReq *request.ChatRequest) (*Command, error) {
	msg := strings.TrimSpace(chatReq.ReqMsg)
	if len(msg) == 0 {
		return nil, pException.ParameterEmptyErrorEx("chatting message is empty")
	}

	if len(msg) > 4096 {
		return nil, pException.ParameterEmptyErrorEx("chatting message is too long")
	}

	// sessionId:统一带单位前缀,且对回传服务端签发 id幂等
	prefix := cCommon.NormalizeCompactId(comCode)
	sessionId := cCommon.NormalizeCompactId(chatReq.SessionId)
	if sessionId == "" {
		sessionId = cCommon.NewCompactId(prefix)
	} else {
		sessionId = cCommon.NormalizeCompactId(prefix + strings.TrimPrefix(sessionId, prefix))
	}

	turnId := cCommon.NormalizeCompactId(chatReq.TurnId)
	if turnId == "" {
		turnId = cCommon.NewCompactId(pCommon.BlankString)
	}

	genAt := chatReq.GenAt
	if genAt <= 0 {
		genAt = pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	}

	chatType, channel := chatReq.ChatType, chatReq.ChatChannel
	if chatType == 0 {
		chatType = cCommon.ChatTypePrivate
	}
	if channel == 0 {
		channel = cCommon.ChatChannelWeb
	}

	return &Command{
		Sender: sender, ComCode: comCode,
		SessionId: sessionId, TurnID: turnId,
		Message: msg,
		AgentID: chatReq.AgentId, ChatType: chatType, ChatChannel: channel, GenAt: genAt,
	}, nil
}

// ToChatRecords 将记忆文件读取出来的MemoryRecord翻译成Record,用在装配时使用
func ToChatRecords(memoryRecord []ctxmgr.MemoryRecord) []Record {
	if len(memoryRecord) == 0 {
		return nil
	}

	out := make([]Record, len(memoryRecord)) // 按长度分配而非append;顺序即下标,0增长
	for i := range memoryRecord {
		r := memoryRecord[i]
		out[i] = Record{
			Index:      r.Index,
			Kind:       r.RecordType,
			Title:      r.Title,
			TimeText:   r.TimeText,
			Content:    r.Content,
			CoversFrom: r.CoversFrom,
			CoversTo:   r.CoversTo,
		}
	}

	return out
}

// ToMemoryRecords ToChatRecords的反向操作
func ToMemoryRecords(records []Record) []ctxmgr.MemoryRecord {
	if len(records) == 0 {
		return nil
	}

	out := make([]ctxmgr.MemoryRecord, len(records))
	for i := range records {
		r := records[i]
		out[i] = ctxmgr.MemoryRecord{
			Index:      r.Index,
			RecordType: r.Kind,
			Title:      r.Title,
			TimeText:   r.TimeText,
			Content:    r.Content,
			CoversFrom: r.CoversFrom,
			CoversTo:   r.CoversTo,
		}
	}
	return out
}

func ToTSession(s Session) dataobject.TSession {
	return dataobject.TSession{
		SessionId: s.ID,
		AgentId:   s.AgentID,
		Sender:    s.Sender,
		ChatData:  s.HistoryRef,
	}
}

func ToTSessionPtr(s Session) *dataobject.TSession {
	return new(ToTSession(s))
}

func ToChatRequest(cmd Command) *request.ChatRequest {
	return &request.ChatRequest{
		SenderId:    cmd.Sender,
		SessionId:   cmd.SessionId,
		TurnId:      cmd.TurnID,
		ReqMsg:      cmd.Message,
		ChatType:    cmd.ChatType,
		ChatChannel: cmd.ChatChannel,
		GenAt:       cmd.GenAt,
		AgentId:     cmd.AgentID,
	}
}

// BindToolInvocation 将模型声明的调用与序号配对
func BindToolInvocation(calls []llmproto.ToolCall, seq *seqSink) []toolexecutor.ToolInvocation {
	out := make([]toolexecutor.ToolInvocation, 0, len(calls))
	seen := make(map[int]struct{}, len(calls))

	for i := range calls {
		key := calls[i].Index
		if _, dup := seen[key]; dup {
			key = -1 - i //负数 -> SeqOf 走独立分配
		}
		seen[key] = struct{}{}

		out = append(out, toolexecutor.ToolInvocation{Seq: seq.SeqOf(key), Call: calls[i]})
	}

	return out
}

func ToTurnTiming(t *TurnTiming) observatory.TurnTiming {
	if t == nil {
		return observatory.TurnTiming{}
	}

	return observatory.TurnTiming{
		TurnScope: t.TurnScope,
		AgentId:   t.AgentId,

		GateMs:   t.ConcurrencyMs, // 唯一需要改名的字段
		RouteMs:  t.RouteMs,
		SkillMs:  t.SkillMs,
		PromptMs: t.PromptMs,
		LlmMs:    t.LlmMs,
		ToolsMs:  t.ToolsMs,

		Rounds:    t.Rounds,
		ToolCalls: t.ToolCalls,
	}
}

var nonRetryMarkers = []string{
	"operation not permitted",    // 沙箱拒绝写
	"denied by configured rules", // 配置的规则拒绝
	"requires an isolated run",   // 沙箱后端不可用
	"no sandbox backend",         //需要沙箱环境,但系统不支持
	"read-only file system",      //
	"permission denied",
}

const noRetryHint = "\n\n[提示]该调用被安全策略或沙箱拒绝,属于确定性限制:同一轮内重试不会成功," +
	"请勿重复同一调用;如需继续,请改用被允许的方式,或向用户说明该限制。"

func needNoRetryHint(o toolexecutor.ToolOutcome) bool {
	if o.Reason == nil {
		return false
	}

	msg := strings.ToLower(o.Reason.Error())
	for _, marker := range nonRetryMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}

	return false
}

// ForModel 返回回填给模型的文本:截断到 maxRunes(省略号计入总数)
// 与 ForRecord(写历史,完整原文)相对:两条互相冲突的消息口径收敛在这个函数里,调用方不需要关心如何截断
func ForModel(o toolexecutor.ToolOutcome, maxRunes int) string {
	text := o.ForRecord()
	if needNoRetryHint(o) {
		// 如果以上原因,可给LLM提示不需要再试了,同一轮内出现以上提示工具不可能执行成功
		text += noRetryHint
	}

	marker := cCommon.TruncatedMarker
	m := utf8.RuneCountInString(marker)
	if maxRunes <= 0 || m >= maxRunes {
		return pCommon.TruncateRunes(o.ForRecord(), maxRunes, marker)
	}

	return pCommon.TruncateRunes(o.ForRecord(), maxRunes-m+1, marker)
}

func buildPartial(content string, calls []llmproto.ToolCall, finish string) llmproto.LlmReply {
	return llmproto.LlmReply{Content: strings.TrimSpace(content), ToolCalls: calls, FinishReason: finish}
}
