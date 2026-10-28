package chat

import (
	"fmt"
	"strings"

	"my.favorite/cclawservice/harness/ctxmgr"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/request"
)

// 解析工具名 -> scripts绝对路径
func lookupScript(env ToolEnv, toolName string) (string, error) {
	toolName = strings.TrimSpace(toolName)
	if len(toolName) == 0 {
		return "", fmt.Errorf("tool name is empty")
	}

	if env.Resolver == nil {
		return "", fmt.Errorf("scripts is empty")
	}

	path, err := env.Resolver.Resolve(toolName)
	if err != nil {
		return "", fmt.Errorf("tool script not registered for tool name %s", toolName)
	}

	if path = strings.TrimSpace(path); path == "" {
		return "", fmt.Errorf("tool script not registered for tool name %s", toolName)
	}

	return path, nil
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

func ToTSession(s Session) dataobject.TSession {
	return dataobject.TSession{
		SessionId: s.ID,
		AgentId:   s.AgentID,
		Sender:    s.Sender,
		ChatData:  string(s.HistoryRef),
	}
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
