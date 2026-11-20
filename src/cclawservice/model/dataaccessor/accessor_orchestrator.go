/**
* 将编排有关的信息落库的类
**/

package dataaccessor

import (
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	exception2 "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/database/core"
	"my.favorite/platformutility/exception"
)

type IOrchestratorAccessor interface {
	GetDatabase() core.IBaseDatabase

	// CheckSchema 启动自检:关键列于唯一索引是否齐备
	CheckSchema() error

	SavePlan(plan *dataobject.TPlan) error
	GetPlan(turnId string) (*dataobject.TPlan, error)
	RemovePlan(turnId string) error

	UpsertTask(plan *dataobject.TPlanTask) error
	ListTasks(turnId string) ([]*dataobject.TPlanTask, error)

	AppendGaps(gaps []dataobject.TPlanGap) error
	ListGaps(turnId string) ([]*dataobject.TPlanGap, error)

	AppendArtifacts(artifacts []dataobject.TArtifact) error
	ListArtifacts(turnId string) ([]*dataobject.TArtifact, error)
	GetNewestArtifact(turnId, name string) (int, error)

	SaveDecision(decision *dataobject.TIntentionDecision) error
}

type OrchestratorAccessor struct {
	BaseAccessor

	gormDb *gorm.DB
}

var orchestratorAccessor = pCommon.GetSingleton(func() *OrchestratorAccessor {
	return &OrchestratorAccessor{}
}).Instance()

func GetOrchestratorAccessor(db core.IBaseDatabase) (IOrchestratorAccessor, error) {
	if db == nil {
		return nil, exception.ParameterEmptyErrorEx("OrchestratorAccessor:database is nil")
	}

	gormDb, ok := db.(*core.GormDatabase)
	if !ok {
		return nil, exception.UnsupportedExceptionErrorEx2("session accessor: database type[%s] is unsupported", db.GetAccessorType())
	}

	orchestratorAccessor.db = db
	orchestratorAccessor.gormDb = gormDb.GetDB()
	return orchestratorAccessor, nil
}

// SavePlan 写入或更新一个Plan,重新调用只是刷新骨架信息和预算
func (db *OrchestratorAccessor) SavePlan(plan *dataobject.TPlan) error {
	if plan == nil {
		return exception.ParameterEmptyErrorEx("OrchestratorAccessor: plan row is nil")
	}

	if strings.TrimSpace(plan.TurnId) == pCommon.BlankString {
		return exception.ParameterEmptyErrorEx("OrchestratorAccessor: empty turnId")
	}

	err := db.gormDb.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "turn_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"session_id", "skill_id", "spec_json", "platter_json", "json_digest", "waves",
			"task_count", "est_tokens", "est_latency_ms", "lint_ok", "updated_at",
		}),
	}).Create(plan).Error
	if err != nil {
		return exception2.DatabaseExecErrEx2("OrchestratorAccessor:save plan error:%s", err.Error())
	}

	return nil
}

func (db *OrchestratorAccessor) GetPlan(turnId string) (*dataobject.TPlan, error) {
	var row dataobject.TPlan

	err := db.gormDb.Where("turn_id = ?", turnId).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, exception2.DatabaseExecErrEx2("OrchestratorAccessor:GetPlan error:%s", err.Error())
	}
	return &row, nil
}

// RemovePlan 删除一轮计划及其从属数据
// 用于SOP变更导致旧计划失效的重置场景
func (db *OrchestratorAccessor) RemovePlan(turnId string) error {
	err := db.gormDb.Transaction(func(d *gorm.DB) error {
		if e := d.Where("turn_id = ?", turnId).Delete(&dataobject.TPlanTask{}).Error; e != nil {
			return e
		}

		if e := d.Where("turn_id = ?", turnId).Delete(&dataobject.TPlanGap{}).Error; e != nil {
			return e
		}

		if e := d.Where("turn_id = ?", turnId).Delete(&dataobject.TArtifact{}).Error; e != nil {
			return e
		}

		return d.Where("turn_id = ?", turnId).Delete(&dataobject.TPlan{}).Error
	})

	if err != nil {
		return exception2.DatabaseExecErrEx2("OrchestratorAccessor:RemovePlan error:%s", err.Error())
	}

	return nil
}

func (db *OrchestratorAccessor) UpsertTask(plan *dataobject.TPlanTask) error {
	if plan == nil {
		return exception.ParameterEmptyErrorEx("OrchestratorAccessor:upsert plan is nil")
	}
	if strings.TrimSpace(plan.TurnId) == pCommon.BlankString || plan.TaskId == pCommon.BlankString {
		return exception.ParameterEmptyErrorEx("OrchestratorAccessor:empty turnId or taskId")
	}

	err := db.gormDb.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "turn_id"}, {Name: "task_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"kind", "action", "section", "status", "tries", "started_at", "ended_at",
			"input_digest", "output_name", "gate_pass", "err", "updated_at",
		}),
	}).Create(plan).Error

	if err != nil {
		return exception2.DatabaseExecErrEx2("OrchestratorAccessor:upsert plan error:%s", err.Error())
	}

	return nil
}

func (db *OrchestratorAccessor) ListTasks(turnId string) ([]*dataobject.TPlanTask, error) {
	list := make([]*dataobject.TPlanTask, 0, 16)

	err := db.gormDb.Where("turn_id = ?", turnId).Order("task_id ASC").Find(&list).Error
	if err != nil {
		return nil, exception2.DatabaseExecErrEx2("OrchestratorAccessor:ListTasks error:%s", err.Error())
	}
	return list, nil
}

func (db *OrchestratorAccessor) AppendGaps(gaps []dataobject.TPlanGap) error {
	if len(gaps) == 0 {
		return nil
	}

	err := db.gormDb.Clauses(clause.OnConflict{
		DoNothing: true,
	}).CreateInBatches(&gaps, 32).Error

	if err != nil {
		return exception2.DatabaseExecErrEx2("OrchestratorAccessor:AppendGaps error:%s", err.Error())
	}

	return nil
}

func (db *OrchestratorAccessor) ListGaps(turnId string) ([]*dataobject.TPlanGap, error) {
	list := make([]*dataobject.TPlanGap, 0, 16)

	err := db.gormDb.Where("turn_id = ?", turnId).Order("id ASX").Find(&list).Error
	if err != nil {
		return nil, exception2.DatabaseExecErrEx2("OrchestratorAccess:list gaps error:%s", err.Error())
	}

	return list, nil
}

func (db *OrchestratorAccessor) AppendArtifacts(artifacts []dataobject.TArtifact) error {
	if len(artifacts) == 0 {
		return nil
	}

	err := db.gormDb.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&artifacts, 32).Error
	if err != nil {
		return exception2.DatabaseExecErrEx2("OrchestratorAccessor:AppendArtifacts error:%s", err.Error())
	}
	return nil
}

func (db *OrchestratorAccessor) ListArtifacts(turnId string) ([]*dataobject.TArtifact, error) {
	list := make([]*dataobject.TArtifact, 0, 16)

	err := db.gormDb.Where("turn_id = ?", turnId).Order("name ASC,rev ASC").Find(&list).Error
	if err != nil {
		return nil, exception2.DatabaseExecErrEx2("OrchestratorAccessor:ListArtifacts error:%s", err.Error())
	}
	return list, nil
}

func (db *OrchestratorAccessor) GetNewestArtifact(turnId, name string) (int, error) {
	var maxVersion int
	row := db.gormDb.Model(&dataobject.TArtifact{}).
		Where("turn_id = ? AND name = ?", turnId, name).
		Select("COALESCE(MAX(rev),0").Row()
	if err := row.Scan(&maxVersion); err != nil {
		return 0, exception2.DatabaseExecErrEx2("OrchestratorAccessor:GetNewestArtifact error:%s", err.Error())
	}

	return maxVersion, nil
}

func (db *OrchestratorAccessor) SaveDecision(decision *dataobject.TIntentionDecision) error {
	if decision == nil {
		return exception.ParameterEmptyErrorEx("OrchestratorAccessor:SaveDecision parameter is nil")
	}

	if strings.TrimSpace(decision.TurnId) == pCommon.BlankString {
		return exception.ParameterEmptyErrorEx("OrchestratorAccessor:SaveDecision parameter turnId is empty")
	}

	err := db.gormDb.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "turn_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"skill_id", "decision", "reason", "missing", "rounds", "elapsed_ms"}),
	}).Create(decision).Error
	if err != nil {
		return exception2.DatabaseExecErrEx2("OrchestratorAccessor:SaveDecision error:%s", err.Error())
	}

	return nil
}
