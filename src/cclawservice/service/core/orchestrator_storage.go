/**
* 编排层面的数据读取和存储服务
*
* 放在service层的原因：
* - chat/sop 不能 import model/sop之外的数据(否则包内必定要起数据库)
* - model/dataaccessor 不能 import chat/sop ,否则就依赖于业务语义
* 本类是唯一认识两侧的地方,放在service包更加合适
*
**/

package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"

	"my.favorite/cclawservice/biz/npa"
	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/model/dataaccessor"
	"my.favorite/cclawservice/model/dataobject"
	chatCommon "my.favorite/cclawservice/runtime/common"
	"my.favorite/cclawservice/runtime/plan_mode"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"
)

const (
	// 单条 gap 明细的落库长度上限,
	planGapDetailMaxRunes = 1024
)

type OrchestratorStorage struct {
	accessor dataaccessor.IOrchestratorAccessor

	lock     sync.Mutex
	versions map[string]int   // turnID|Name -> 已分配的最大的version
	firstSee map[string]int64 // turnID|Name -> started_at
}

var orchestratorStorage = pCommon.GetSingleton(func() *OrchestratorStorage {
	return &OrchestratorStorage{
		versions: make(map[string]int, 16),
		firstSee: make(map[string]int64, 32),
	}
}).Instance()

func GetOrchestratorStorage(accessor dataaccessor.IOrchestratorAccessor) *OrchestratorStorage {
	orchestratorStorage.accessor = accessor
	return orchestratorStorage
}

// 编译期间检查:必须同时满足写端口与读端口
var (
	_ plan_mode.IPlanStorage = (*OrchestratorStorage)(nil)
)

func (o *OrchestratorStorage) Ready() bool {
	return o != nil && o.accessor != nil
}

func (o *OrchestratorStorage) Load(ctx context.Context, sessionId, turnId string) (*plan_mode.PlanResumer, error) {
	if !o.Ready() {
		return nil, nil
	}

	plan, err := o.accessor.GetPlan(turnId)
	if err != nil {
		return nil, err
	}

	if plan == nil {
		return nil, nil
	}

	if session := strings.TrimSpace(sessionId); session != pCommon.BlankString && plan.SessionId != session {
		logging.LogWarnF("orchestratorStorage: turn belongs to another session,drop,turn=%s,owner=%s,current=%s",
			turnId, plan.SessionId, session)
		return nil, nil
	}

	var spec npa.ReportSpec
	if e := pCommon.UnmarshalJson(plan.SpecJson, &spec); e != nil {
		logging.LogWarnF("orchestrator storage:broken spec_json,turn=%s,error=%s", turnId, e.Error())
		return nil, nil
	}

	state := plan_mode.NewPlanState(turnId, plan.SessionId, plan.SkillId, spec)
	tasks, err := o.accessor.ListTasks(plan.SessionId, turnId)
	if err != nil {
		return nil, err
	}

	for _, task := range tasks {
		state.TaskStates[task.TaskId] = plan_mode.PlanTaskState{
			TaskID:   task.TaskId,
			Status:   task.Status,
			Tries:    task.Tries,
			Artifact: task.OutputName,
			GatePass: task.GatePass,
			Err:      task.Err,
		}
	}

	artifacts, err := o.accessor.ListArtifacts(turnId)
	if err != nil {
		return nil, err
	}

	for _, artifact := range artifacts {
		fields := make(map[string]string, 4)
		if artifact.FieldsJson != pCommon.BlankString {
			if e := pCommon.UnmarshalJson(artifact.FieldsJson, &fields); e != nil {
				logging.LogWarnF("orchestrator storage:broken fields_json,turn=%s,name=%s,error=%s",
					turnId, artifact.Name, e.Error())
				fields = make(map[string]string, 1)
			}
		}

		content := pCommon.BlankString
		if value, hit := fields[plan_mode.ArtifactContentField]; hit {
			content = value
			delete(fields, plan_mode.ArtifactContentField)
		}

		// 同名多 version:按 version生序覆盖,最终留下最新版本
		state.Artifacts[artifact.Name] = plan_mode.Artifact{
			Name:         artifact.Name,
			ArtifactKind: artifact.Kind,
			AbsolutePath: artifact.Ref,
			Context:      content,
			Digest:       artifact.Digest,
			Fields:       fields,
		}
	}

	gaps, err := o.accessor.ListGaps(turnId)
	if err != nil {
		return nil, err
	}

	for _, gap := range gaps {
		state.Gaps = append(state.Gaps, plan_mode.Gap{
			Code:     gap.Code,
			TaskID:   gap.TaskId,
			Section:  gap.Section,
			Detail:   gap.Detail,
			Severity: plan_mode.ParseSeverity(gap.Severity),
		})
		if gap.Round > state.Round {
			state.Round = gap.Round
		}
	}

	return &plan_mode.PlanResumer{
		PlanDigest: plan.JsonDigest,
		PlanJson:   plan.PlattenJson,
		SpecJson:   plan.SpecJson,
		SpecFields: spec.Fields(),
		State:      state,
	}, nil
}

func (o *OrchestratorStorage) SavePlan(ctx context.Context, turnID, sessionID string, plan plan_mode.Plan, spec chatCommon.Spec) error {
	if !o.Ready() {
		return nil
	}

	planJson, err := plan_mode.MarshalPlanToJson(plan)
	if err != nil {
		return err
	}

	specJson, err := json.Marshal(spec)
	if err != nil {
		return err
	}

	now := pCommon.GetDatetimeUtilityInstance().GetNow().Unix()

	o.lock.Lock()
	o.resetPlanCache(turnID)
	o.lock.Unlock()

	return o.accessor.SavePlan(&dataobject.TPlan{
		TurnId:       turnID,
		SessionId:    sessionID,
		SkillId:      plan.SkillID,
		SpecJson:     string(specJson),
		PlattenJson:  planJson,
		JsonDigest:   plan_mode.GetFingerprintOfPlan(plan),
		Waves:        plan.Waves,
		TaskCount:    plan.TaskCount,
		EstTokens:    plan.EstTokens,
		EstLatencyMs: plan.EstLatency.Milliseconds(),
		CreatedAt:    now,
		UpdatedAt:    now,
	})
}

func (o *OrchestratorStorage) SaveTask(ctx context.Context, turnID string, planTask plan_mode.PlanTask, state plan_mode.PlanTaskState, inputDigest string) error {
	if !o.Ready() {
		return nil
	}

	now := pCommon.GetDatetimeUtilityInstance().GetNow().Unix()
	o.lock.Lock()
	key := turnID + "|" + planTask.ID
	startedAt, seen := o.firstSee[key]
	if !seen {
		startedAt = now
		o.firstSee[key] = now
	}
	o.lock.Unlock()

	endedAt := int64(0)
	if plan_mode.IsTerminal(state.Status) {
		endedAt = now
	}

	err := o.accessor.UpsertTask(&dataobject.TPlanTask{
		TurnId:      turnID,
		TaskId:      planTask.ID,
		Kind:        planTask.Kind.String(),
		Action:      planTask.Action,
		Section:     planTask.Section,
		Status:      state.Status,
		Tries:       state.Tries,
		StartedAt:   startedAt,
		EndedAt:     endedAt,
		InputDigest: inputDigest,
		OutputName:  state.Artifact,
		GatePass:    state.GatePass,
		Err:         pCommon.TruncateRunes(state.Err, planGapDetailMaxRunes, cCommon.TruncatedMarker),
	})

	if err != nil {
		logging.LogWarnF("orchestrator storage:save task failed,turn=%s,task=%s,error=%s",
			turnID, planTask.ID, err.Error())
	}

	return nil
}

func (o *OrchestratorStorage) SaveGaps(ctx context.Context, turnID string, round int, gaps []plan_mode.Gap) error {
	if !o.Ready() || len(gaps) == 0 {
		return nil
	}

	now := pCommon.GetDatetimeUtilityInstance().GetNow().Unix()
	rows := make([]dataobject.TPlanGap, 0, len(gaps))
	for _, gap := range gaps {
		rows = append(rows, dataobject.TPlanGap{
			TurnId:    turnID,
			Round:     round,
			Code:      gap.Code,
			TaskId:    gap.TaskID,
			Section:   gap.Section,
			Severity:  gap.Severity.String(),
			Detail:    pCommon.TruncateRunes(gap.Detail, planGapDetailMaxRunes, cCommon.TruncatedMarker),
			CreatedAt: now,
		})
	}

	if err := o.accessor.AppendGaps(rows); err != nil {
		logging.LogWarnF("orchestrator storage:save gaps failed,turn=%s,count=%d,error=%s",
			turnID, len(rows), err.Error())
	}

	return nil
}

func (o *OrchestratorStorage) SaveArtifact(ctx context.Context, turnID string, artifact plan_mode.Artifact) error {
	if !o.Ready() || artifact.Name == pCommon.BlankString {
		return nil
	}

	ref := artifact.AbsolutePath
	size := int64(0)
	fields := make(map[string]string, len(artifact.Fields)+1)
	for key, value := range artifact.Fields {
		fields[key] = value
	}

	if artifact.ArtifactKind == plan_mode.ArtifactKindText {
		ref = pCommon.BlankString
		size = int64(len(artifact.Context))
		if artifact.Context != pCommon.BlankString {
			fields[plan_mode.ArtifactContentField] = artifact.Context
		}
	} else if artifact.AbsolutePath != pCommon.BlankString {

		info, statErr := os.Stat(artifact.AbsolutePath)
		switch {
		case statErr != nil:
			logging.LogWarnF("orchestrator storage:stat artifact failed,turn=%s,name=%s,path=%s,error=%s",
				turnID, artifact.Name, artifact.AbsolutePath, statErr.Error())
		case info.IsDir():
			logging.LogWarnF("orchestrator storage:artifact path is a directory,turn=%s,name=%s,path=%s",
				turnID, artifact.Name, artifact.AbsolutePath)
		default:
			size = info.Size()
		}
	}

	fieldsJson := pCommon.BlankString
	if len(fields) > 0 {
		if data, err := pCommon.MarshalJson(fields); err == nil {
			fieldsJson = data
		} else {
			logging.LogWarnF("orchestrator storage:marshal artifact fields failed,turn=%s,name=%s,error=%s",
				turnID, artifact.Name, err.Error())
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		nextVer, err := o.nextVersion(turnID, artifact.Name, attempt > 0)
		if err != nil {
			logging.LogErrorF("orchestrator storage:alloc rev failed,turn=%s,name=%s,error=%s",
				turnID, artifact.Name, err.Error())
			return err
		}

		err = o.accessor.AppendArtifacts([]dataobject.TArtifact{{
			TurnId:     turnID,
			Name:       artifact.Name,
			Rev:        nextVer,
			Kind:       artifact.ArtifactKind,
			Ref:        ref,
			Digest:     artifact.Digest,
			Size:       size,
			FieldsJson: fieldsJson,
			CreatedAt:  pCommon.GetDatetimeUtilityInstance().GetNow().Unix(),
		}})
		if err == nil {
			return nil // 成功
		}

		if attempt == 1 {
			logging.LogErrorF("orchestrator storage:append artifact failed,turn=%s,name=%s,rev=%d,error=%s",
				turnID, artifact.Name, nextVer, err.Error())
		}
	}

	return nil
}

// SaveDecision 写协商审计,失败只记日志
func (o *OrchestratorStorage) SaveDecision(ctx context.Context, decision dataobject.TIntentionDecision) {
	if !o.Ready() {
		return
	}

	decision.CreatedAt = pCommon.GetDatetimeUtilityInstance().GetNow().Unix()
	if err := o.accessor.SaveDecision(&decision); err != nil {
		logging.LogWarnF("orchestrator storage:save decision failed,turn=%s,error=%s",
			decision.TurnId, err.Error())
	}
}

// ==================================== private functions & methods =================================
func (o *OrchestratorStorage) resetPlanCache(turnID string) {
	prefix := turnID + "|"

	for key := range o.versions {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			delete(o.versions, key)
		}
	}

	for key := range o.firstSee {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			delete(o.firstSee, key)
		}
	}
}

func (o *OrchestratorStorage) nextVersion(turnID, name string, refresh bool) (int, error) {
	key := turnID + "|" + name

	o.lock.Lock()
	defer o.lock.Unlock()

	if !refresh {
		if version, ok := o.versions[key]; ok {
			o.versions[key] = version + 1
			return version + 1, nil
		}
	}

	maxVersion, err := o.accessor.GetNewestArtifact(turnID, name)
	if err != nil {
		return 0, err
	}

	o.versions[key] = maxVersion + 1
	return maxVersion + 1, nil
}

// 防止 以下两包在后续重构中变成未使用
var (
	_ = errors.Is
	_ = pCommon.BlankString
)
