/**
* 会话管理模块的数据对象(阶段1)
*
* 严格对齐 data/ddl_sqlite3.sql 的三张表:
*   - TSession   → t_session   会话(元数据 + 历史文件名)
*   - TTurn      → t_turn      轮次(一问一答,记录在历史文件中的索引)
*   - TTurnTool  → t_turn_tool 该轮调用过的工具(顺序号 + 返回值摘要)
*
* 设计要点:会话内容不落库,而是写入工作区的 memory_{yyyyMMddHHmmss}.md;
* 表内只保存"索引/元数据":TSession.ChatData = 历史文件名,
* TTurn.ChatIndex/RespIndex = 用户问题/助手回复在该文件中的记录序号。
*
* 字段命名沿用既有约定:显式 gorm column 标签 + 与列名一致的 Json 标签。
**/

package dataobject

// 会话结束原因(t_session.end_reason)
const (
	SessionEndReasonIdle    = 0 // 空闲超时
	SessionEndReasonManual  = 1 // 手动结束
	SessionEndReasonCompact = 2 // 触发 compact
)

// TSession 会话:一次完整对话会话的元数据。
// 业务主键为 SessionId(DDL 中 ID 仅作自增主键);ChatData 保存内容文件名,不含目录。
type TSession struct {
	Id                int64  `gorm:"column:id;primaryKey" json:"Id"`                     // 自增主键
	SessionId         string `gorm:"column:session_id" json:"SessionId"`                 // 会话业务主键(<=32 字符)
	SessionName       string `gorm:"column:session_name" json:"SessionName"`             // 会话名称(首条消息截断生成)
	AgentId           string `gorm:"column:agent_id" json:"AgentId"`                     // 会话绑定的 Agent(替代仅内存存在的绑定)
	BeginTimestamp    int64  `gorm:"column:begin_timestamp" json:"BeginTimestamp"`       // 会话开始时间(Unix 秒)
	ChatType          int    `gorm:"column:chat_type" json:"ChatType"`                   // 0/1:私聊/群聊
	ChatChannel       int    `gorm:"column:chat_channel" json:"ChatChannel"`             // 聊天渠道
	Sender            string `gorm:"column:sender" json:"Sender"`                        // 创建会话的用户名(服务端认证)
	Archived          bool   `gorm:"column:archived" json:"Archived"`                    // 是否归档
	ArchivedTimestamp int64  `gorm:"column:archived_timestamp" json:"ArchivedTimestamp"` // 归档时间
	ChatData          string `gorm:"column:chat_data" json:"ChatData"`                   // 聊天历史文件名(memory_xxx.md)
	Ended             bool   `gorm:"column:ended" json:"Ended"`                          // 是否已结束
	EndReason         int    `gorm:"column:end_reason" json:"EndReason"`                 // 见 SessionEndReason*
	EndTimestamp      int64  `gorm:"column:end_timestamp" json:"EndTimestamp"`           // 结束时间
	Removed           bool   `gorm:"column:removed" json:"Removed"`                      // 是否删除(软删除)
}

// TableName 显式声明表名,避免命名策略差异导致指错表。
func (TSession) TableName() string { return "t_session" }

// TTurn 轮次:一次"用户提问 + 助手回复"。
// Seq 为会话内轮次序号(从 1 开始);ChatIndex/RespIndex 指向历史文件中的记录序号。
type TTurn struct {
	Id        int64  `gorm:"column:id;primaryKey" json:"Id"`     // 自增主键
	TurnId    string `gorm:"column:turn_id" json:"TurnId"`       // 轮次业务主键(<=32 字符)
	SessionId string `gorm:"column:session_id" json:"SessionId"` // 所属会话
	Seq       int    `gorm:"column:seq" json:"Seq"`              // 会话内轮次序号(从 1 开始)
	ChatTime  int64  `gorm:"column:chat_time" json:"ChatTime"`   // 提问时间(Unix 秒)
	ChatIndex int    `gorm:"column:chat_index" json:"ChatIndex"` // 问题在历史文件中的记录序号
	RespTime  int64  `gorm:"column:resp_time" json:"RespTime"`   // 回复时间(Unix 秒)
	RespIndex int    `gorm:"column:resp_index" json:"RespIndex"` // 回复在历史文件中的记录序号
}

// TableName 显式声明表名。
func (TTurn) TableName() string { return "t_turn" }

// TTurnTool 一轮中调用过的一次工具。
// Seq 为该轮内工具调用顺序号(从 1 开始),ToolResp 只保存可读摘要(完整输出在历史文件中)。
type TTurnTool struct {
	Id         int64  `gorm:"column:id;primaryKey" json:"Id"`       // 自增主键
	TurnId     string `gorm:"column:turn_id" json:"TurnId"`         // 所属轮次
	Seq        int    `gorm:"column:seq" json:"Seq"`                // 轮内工具顺序号(从 1 开始)
	ToolCalled bool   `gorm:"column:tool_called" json:"ToolCalled"` // 是否实际调用了工具(失败亦记 1)
	CallTime   int64  `gorm:"column:call_time" json:"CallTime"`     // 调用时间(Unix 秒)
	ToolName   string `gorm:"column:tool_name" json:"ToolName"`     // 工具名称
	ToolResp   string `gorm:"column:tool_resp" json:"ToolResp"`     // 返回值摘要(截断至 256 字符)
	RespTime   int64  `gorm:"column:resp_time" json:"RespTime"`     // 返回时间(Unix 秒)
}

// TableName 显式声明表名。
func (TTurnTool) TableName() string { return "t_turn_tool" }
