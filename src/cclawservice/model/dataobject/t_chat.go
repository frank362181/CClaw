/**
* 会话管理模块的数据对象
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

// 轮次落库状态(t_turn.status):回答"这一轮有没有落成助手记录、为什么"
const (
	TurnStatusNormal             = 0 // 正常:助手记录已落盘
	TurnStatusInterruptedEmpty   = 1 // 中断且无产出:按设计不写助手记录(空记录无信息量)
	TurnStatusInterruptedPartial = 2 // 中断但有部分产出:已落 assistant:interrupted 记录
	TurnStatusRespNotWritten     = 3 // 助手记录未落盘:写失败,或该会话当时无历史文件(看 chat_index 区分)
)

// InterruptReason 未正常结束的来源(t_turn.interrupt_reason)。
//
// 与 status **正交**:status 说"回复有没有落盘",它说"谁/什么打断了本轮"。
// 为什么不定成字符串:枚举要能稳定聚合统计(日志文案会改,数值不会)。
type InterruptReason int

const (
	InterruptNone     InterruptReason = 0 // 未中断(正常完成)
	InterruptByUser   InterruptReason = 1 // 用户主动点中断(中断 API 命中)
	InterruptByClient InterruptReason = 2 // 客户端断开(关页面/断网)
	InterruptTimeout  InterruptReason = 3 // 应用级运行超时(chatRunTimeout)
	InterruptUpstream InterruptReason = 4 // 上游流被取消
	InterruptOther    InterruptReason = 5 // 其它错误(上游报错/流错误导致残缺)
	InterruptLegacy   InterruptReason = 6 // 历史数据:来源未区分(旧日志把用户中断与客户端断开合并成一句)
)

// String 返回人读文案(日志用;落库用数值)。
func (r InterruptReason) String() string {
	switch r {
	case InterruptByUser:
		return "interrupted by user"
	case InterruptByClient:
		return "client disconnected"
	case InterruptTimeout:
		return "run timeout"
	case InterruptUpstream:
		return "upstream stream canceled"
	case InterruptOther:
		return "stream/upstream error"
	case InterruptLegacy:
		return "interrupted (legacy, source unknown)"
	default:
		return "not interrupted"
	}
}

// TTurn 轮次:一次"用户提问 + 助手回复"。
// Seq 为会话内轮次序号(从 1 开始);ChatIndex/RespIndex 指向历史文件中的记录序号。
type TTurn struct {
	Id              int64           `gorm:"column:id;primaryKey" json:"Id"`                 // 自增主键
	TurnId          string          `gorm:"column:turn_id" json:"TurnId"`                   // 轮次业务主键(<=32 字符)
	SessionId       string          `gorm:"column:session_id" json:"SessionId"`             // 所属会话
	Seq             int             `gorm:"column:seq" json:"Seq"`                          // 会话内轮次序号(从 1 开始)
	ChatTime        int64           `gorm:"column:chat_time" json:"ChatTime"`               // 提问时间(Unix 秒)
	ChatIndex       int             `gorm:"column:chat_index" json:"ChatIndex"`             // 问题在历史文件中的记录序号
	RespTime        int64           `gorm:"column:resp_time" json:"RespTime"`               // 回复时间(Unix 秒)
	RespIndex       int             `gorm:"column:resp_index" json:"RespIndex"`             // 回复在历史文件中的记录序号(0=未落盘)
	Status          int             `gorm:"column:status" json:"Status"`                    // 见 TurnStatus*
	InterruptReason InterruptReason `gorm:"column:interrupt_reason" json:"InterruptReason"` // 见 Interrupt*
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

// SessionTurnView 轮次会话的视图
type SessionTurnView struct {
	TurnId            string          `json:"turnId"`            // 轮次主键
	Seq               int             `json:"seq"`               // 会话内轮次的次序
	ChatTime          int64           `json:"chatTime"`          // 提问时间
	RespTime          int64           `json:"respTime"`          // 回答时间
	Status            int             `json:"status"`            // 见TurnStatus
	InterruptReason   InterruptReason `json:"InterruptReason"`   // 见Interrupt
	Question          string          `json:"question"`          // 提问正文
	Answer            string          `json:"answer"`            // 回复正文
	AnswerInterrupted bool            `json:"AnswerInterrupted"` // 是否被中断的标志
}
