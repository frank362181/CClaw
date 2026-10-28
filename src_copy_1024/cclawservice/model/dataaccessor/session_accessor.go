/**
* 会话管理模块的数据访问层(阶段1)
*
* 对应 data/ddl_sqlite3.sql 的 t_session / t_turn / t_turn_tool 三张表。
*
* 为什么单独一个 accessor(而不是并入 IAccessor):
*   - IAccessor 承载的是"配置类"数据(LLM/Agent/Skill/路由规则),被 AgentRepository
*     以 SafeList 全量缓存在进程内、按 TTL 刷新;
*   - 会话/轮次/工具记录是"高吞吐事务性"数据,数量随使用量线性增长,
*     既不适合全量缓存,也不适合放进同一接口(会让配置访问器被迫承担无关职责)。
*   因此这里独立定义 ISessionAccessor,并沿用 GetGormAccessor 的"仅支持 gorm"约定。
**/

package dataaccessor

import (
	"errors"

	"gorm.io/gorm"
	"my.favorite/cclawservice/common"
	exception2 "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/database/core"
	"my.favorite/platformutility/exception"
)

// ISessionAccessor 会话管理模块的数据访问接口。
// 读取类方法约定:未命中返回 (nil, nil),由调用方判空(与 GetUserByAccount 保持一致)。
type ISessionAccessor interface {
	GetDatabase() core.IBaseDatabase

	// CreateSession ------------------------------ 会话(t_session) ------------------------------
	CreateSession(session *dataobject.TSession) error
	GetSession(sessionId string) (*dataobject.TSession, error)
	ListSessions(sender string, limit int) ([]dataobject.TSession, error)
	SetSessionChatData(sessionId, chatData string) error
	SetSessionArchived(sessionId string, archived bool, timestamp int64) error
	SetSessionEnded(sessionId string, reason int, timestamp int64) error
	DeleteSession(sessionId string) error

	// CreateTurn ------------------------------ 轮次(t_turn) ------------------------------
	CreateTurn(turn *dataobject.TTurn) error
	GetTurn(turnId string) (*dataobject.TTurn, error)
	GetTurns(sessionId string) ([]dataobject.TTurn, error)
	GetMaxTurnSeq(sessionId string) (int, error)
	GetMaxRespIndex(sessionId string) (int, error)

	// CreateTurnTools --------------------------- 轮内工具(t_turn_tool) ---------------------------
	CreateTurnTools(tools []dataobject.TTurnTool) error
	GetTurnTools(turnId string) ([]dataobject.TTurnTool, error)
}

// SessionAccessor 基于 gorm 的会话数据访问实现。
type SessionAccessor struct {
	BaseAccessor

	gormDb *gorm.DB
}

var sessionAccessor = pCommon.GetSingleton(func() *SessionAccessor {
	return &SessionAccessor{
		BaseAccessor: BaseAccessor{
			db: nil,
		},
	}
}).Instance()

// GetSessionAccessor 构建会话数据访问器。仅支持 gorm 实现(与 GetGormAccessor 一致)。
func GetSessionAccessor(db core.IBaseDatabase) (ISessionAccessor, error) {
	if db == nil {
		return nil, exception.ParameterEmptyErrorEx("session accessor: database is nil")
	}

	gormDb, ok := db.(*core.GormDatabase)
	if !ok {
		return nil, exception.UnsupportedExceptionErrorEx2("session accessor: database type[%s] is unsupported", db.GetAccessorType())
	}

	sessionAccessor.db = db
	sessionAccessor.gormDb = gormDb.GetDB()
	return sessionAccessor, nil
}

// ============================== 会话(t_session) ==============================

// CreateSession 新建会话记录。session_id 冲突(唯一键)时返回数据库错误,
// 由上层按"已存在即复用"处理,避免并发建档互相覆盖。
func (g *SessionAccessor) CreateSession(session *dataobject.TSession) error {
	if session == nil {
		return exception.ParameterEmptyErrorEx("session is nil")
	}

	if err := g.gormDb.Create(session).Error; err != nil {
		return exception2.DatabaseQueryErrEx2("create session error:%s", err.Error())
	}
	return nil
}

// GetSession 按会话业务主键读取未删除的会话;未命中返回 (nil, nil)。
func (g *SessionAccessor) GetSession(sessionId string) (*dataobject.TSession, error) {
	var session dataobject.TSession
	err := g.gormDb.Where("session_id = ? AND removed = 0", sessionId).First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, exception2.DatabaseQueryErrEx2("load session error:%s", err.Error())
	}
	return &session, nil
}

// ListSessions 列出某用户(服务端认证的用户名)的会话,按开始时间倒序。
func (g *SessionAccessor) ListSessions(sender string, limit int) ([]dataobject.TSession, error) {
	if limit <= 0 {
		limit = common.DefaultRecordsLimit
	}

	var list []dataobject.TSession
	err := g.gormDb.Where("sender = ? AND removed = 0", sender).
		Order("begin_timestamp DESC").
		Limit(limit).
		Find(&list).Error
	if err != nil {
		return nil, exception2.DatabaseQueryErrEx2("list sessions error:%s", err.Error())
	}
	return list, nil
}

// SetSessionChatData 回写会话历史文件名(t_session.chat_data)。
func (g *SessionAccessor) SetSessionChatData(sessionId, chatData string) error {
	return g.updateSession(sessionId, map[string]interface{}{"chat_data": chatData})
}

// SetSessionArchived 设置归档状态与归档时间。
func (g *SessionAccessor) SetSessionArchived(sessionId string, archived bool, timestamp int64) error {
	return g.updateSession(sessionId, map[string]interface{}{
		"archived":           archived,
		"archived_timestamp": timestamp,
	})
}

// SetSessionEnded 结束会话:写入结束标记/原因(见 dataobject.SessionEndReason*)/时间。
func (g *SessionAccessor) SetSessionEnded(sessionId string, reason int, timestamp int64) error {
	return g.updateSession(sessionId, map[string]interface{}{
		"ended":         true,
		"end_reason":    reason,
		"end_timestamp": timestamp,
	})
}

// DeleteSession 软删除会话(removed=1)。轮次/工具记录保留,便于审计与恢复。
func (g *SessionAccessor) DeleteSession(sessionId string) error {
	return g.updateSession(sessionId, map[string]interface{}{"removed": true})
}

// updateSession 会话字段更新(会话表无乐观锁列,按业务主键直接更新)。
func (g *SessionAccessor) updateSession(sessionId string, values map[string]interface{}) error {
	if len(values) == 0 {
		return nil
	}

	res := g.gormDb.Model(&dataobject.TSession{}).
		Where("session_id = ? AND removed = 0", sessionId).
		Updates(values)
	if res.Error != nil {
		return exception2.DatabaseQueryErrEx2("update session error:%s", res.Error.Error())
	}
	if res.RowsAffected == 0 {
		return exception.UnMatchErrorEx2("session not found: %s", sessionId)
	}
	return nil
}

// ============================== 轮次(t_turn) ==============================

// CreateTurn 写入一条轮次记录。(session_id, seq) 上有唯一索引,重复写入会返回错误,
// 作为"同一轮被重复落库"的最后一道防线。
func (g *SessionAccessor) CreateTurn(turn *dataobject.TTurn) error {
	if turn == nil {
		return exception.ParameterEmptyErrorEx("turn is nil")
	}

	if err := g.gormDb.Create(turn).Error; err != nil {
		return exception2.DatabaseQueryErrEx2("create turn error:%s", err.Error())
	}
	return nil
}

// GetTurn 按轮次业务主键读取;未命中返回 (nil, nil)。
// 供"同一 turnId 重复上报"的幂等判断使用(必须早于历史文件追加,否则会写重)。
func (g *SessionAccessor) GetTurn(turnId string) (*dataobject.TTurn, error) {
	var turn dataobject.TTurn
	err := g.gormDb.Where("turn_id = ?", turnId).First(&turn).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, exception2.DatabaseQueryErrEx2("load turn error:%s", err.Error())
	}
	return &turn, nil
}

// GetTurns 按轮次序号升序读取某会话的全部轮次。
func (g *SessionAccessor) GetTurns(sessionId string) ([]dataobject.TTurn, error) {
	var list []dataobject.TTurn
	err := g.gormDb.Where("session_id = ?", sessionId).Order("seq ASC").Find(&list).Error
	if err != nil {
		return nil, exception2.DatabaseQueryErrEx2("load turns error:%s", err.Error())
	}
	return list, nil
}

// GetMaxTurnSeq 返回某会话当前最大轮次序号;无记录返回 0。
// 调用方需持有会话锁后再调用(读最大值 + 1 的分配逻辑必须串行)。
func (g *SessionAccessor) GetMaxTurnSeq(sessionId string) (int, error) {
	var maxSeq int
	row := g.gormDb.Model(&dataobject.TTurn{}).
		Where("session_id = ?", sessionId).
		Select("COALESCE(MAX(seq), 0)").
		Row()
	if err := row.Scan(&maxSeq); err != nil {
		return 0, exception2.DatabaseQueryErrEx2("max turn seq error:%s", err.Error())
	}
	return maxSeq, nil
}

// GetMaxRespIndex 返回某会话历史上已消耗的最大记录序号(取 resp_index 的最大值);无记录返回 0。
//
// 为什么用 resp_index 就能代表"已消耗的最大序号":
// 每轮记录的写入顺序固定为 用户问题 → 工具调用… → 助手回复,
// 所以助手回复的序号必定是该轮最大的;会话级取 MAX 即全局最大值。
// 它是历史文件"下一个记录序号"的权威来源(见 ctxmgr.HistoryFileWriter 的序号契约),
// 比扫描文件可靠(压缩后文件变小也不影响),且有 idx_t_turn_session_id 支撑,成本 O(1)。
func (g *SessionAccessor) GetMaxRespIndex(sessionId string) (int, error) {
	var maxIndex int
	row := g.gormDb.Model(&dataobject.TTurn{}).
		Where("session_id = ?", sessionId).
		Select("COALESCE(MAX(resp_index), 0)").
		Row()
	if err := row.Scan(&maxIndex); err != nil {
		return 0, exception2.DatabaseQueryErrEx2("max resp index error:%s", err.Error())
	}
	return maxIndex, nil
}

// ============================== 轮内工具(t_turn_tool) ==============================

// CreateTurnTools 批量写入一轮内的工具调用记录(无工具时静默返回)。
func (g *SessionAccessor) CreateTurnTools(tools []dataobject.TTurnTool) error {
	if len(tools) == 0 {
		return nil
	}

	if err := g.gormDb.Create(&tools).Error; err != nil {
		return exception2.DatabaseQueryErrEx2("create turn tools error:%s", err.Error())
	}
	return nil
}

// GetTurnTools 按轮内顺序号升序读取某轮的工具调用记录。
func (g *SessionAccessor) GetTurnTools(turnId string) ([]dataobject.TTurnTool, error) {
	var list []dataobject.TTurnTool
	err := g.gormDb.Where("turn_id = ?", turnId).Order("seq ASC").Find(&list).Error
	if err != nil {
		return nil, exception2.DatabaseQueryErrEx2("load turn tools error:%s", err.Error())
	}
	return list, nil
}
