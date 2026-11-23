/**
* plan_mode_engine引擎
*
* 从chatting_service.go中收拢的方法实现plan mode下的功能
**/

package plan_mode

import (
	"context"
	"fmt"
	"strings"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/config"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/toolexecutor"
	"my.favorite/cclawservice/model/dataobject"
	chatCommon "my.favorite/cclawservice/runtime/common"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/concurrency"
	"my.favorite/platformutility/logging"
)

// PlanModeEngineParams engine的参数注入
// 由service组合注入
type PlanModeEngineParams struct {
	Config     config.ChatConfig
	Negotiator INegotiator // nil -> NopNegotiator
	Storage    IPlanStorage
	ConGate    concurrency.IConcurrencyController
	Extractors *Extractors
	Barriers   *BarrierRepository
}

type PlanModeEngine struct {
	chatConfig config.ChatConfig
	negotiator INegotiator
	conGate    concurrency.IConcurrencyController
	storage    IPlanStorage
	extractors *Extractors
	barriers   *BarrierRepository
}

func NewPlanModeEngine(engineParams PlanModeEngineParams) *PlanModeEngine {
	negotiator := engineParams.Negotiator
	if negotiator == nil {
		negotiator = NopNegotiator{}
	}

	extractors := engineParams.Extractors
	if extractors == nil {
		extractors = NewExtractors()
	}

	barriers := engineParams.Barriers
	if barriers == nil {
		barriers = NewBarrierRepository()
	}
	storage := engineParams.Storage
	if storage == nil {
		storage = NopPlanStorage{}
	}
	return &PlanModeEngine{
		chatConfig: engineParams.Config,
		negotiator: negotiator,
		conGate:    engineParams.ConGate,
		storage:    storage,
		extractors: extractors,
		barriers:   barriers,
	}
}

func (engine *PlanModeEngine) NegotiateArgument(ctx context.Context, input NegotiationInput) NegotiationOutput {
	if input.Sink == nil {
		input.Sink = cCommon.NopSink{}
	}

	if input.NegSpec == nil {
		// 无业务规约,该技能不支持plan mode
		logging.LogWarnF("plan mode:no negotiation spec,skill=%s,turn=%s", input.SkillID, input.TurnID)
		return NegotiationOutput{}
	}

	var outcome NegotiationOutput
	if engine.chatConfig.Chat.Mode == cCommon.PlanModeForce {
		outcome = engine.forceSpec(ctx, input)
	} else {
		outcome = engine.negotiateSpec(ctx, input)
	}

	if !outcome.Ready {
		return outcome
	}

	persisted := engine.loadSpecFields(ctx, input.SessionID, input.TurnID)
	ensuedID := input.NegSpec.EnsureID(outcome.Spec, persisted)
	if SpecFieldsChanged(outcome.Spec, ensuedID) {
		if issues := input.NegSpec.Validate(ensuedID, input.SkillDoc.FrontMatter); len(issues) > 0 {
			logging.LogWarnF("plan mode: spec still invalid after system fields filled,"+
				"turn=%s,skill=%s,issues=%v; Validate must NOT require system-generated fields",
				input.TurnID, input.SkillID, issues)
		}
	}

	outcome.Spec = ensuedID
	return outcome
}

func (engine *PlanModeEngine) Run(ctx context.Context, engineInput PlanModeEngineInput) (PlanModeEngineOutput, error) {
	sink := engineInput.Sink
	if sink == nil {
		sink = cCommon.NopSink{}
	}

	runner := NewPlanChatBotRunner(PlanChatBotParams{
		ConGate:    engine.conGate,
		Sink:       sink,
		Extractors: engine.extractors,
		Barriers:   engine.barriers,
		PlanStorge: engine.storage,
	})

	planResult, err := runner.Run(ctx, PlanRequest{
		AgentId:      engineInput.AgentID,
		TurnID:       engineInput.TurnID,
		SessionID:    engineInput.SessionID,
		PlanMem:      engineInput.Plan,
		Spec:         engineInput.Spec,
		Sink:         NewNegotiationEventSink(sink, len(engineInput.Plan.Tasks)),
		Tool:         engineInput.Tool,
		ResumedState: engine.loadResumeState(ctx, engineInput.SessionID, engineInput.TurnID, engineInput.Plan),
		StartAt:      engineInput.StartAt,
	})

	if err != nil {
		return PlanModeEngineOutput{}, err
	}

	return PlanModeEngineOutput{
		Answer: GetSOPChatResultSummary(planResult, len(engineInput.Plan.Tasks)),
		State:  planResult.State,
		Gaps:   planResult.Gaps,
		Waves:  planResult.Waves,
	}, nil
}

// PreviewPlan 把SOP大纲展开成计划,并向用户播报预览
// 展开失败不会回退为错误,而是降级为单轮(返回 ok = false)
func (engine *PlanModeEngine) PreviewPlan(turnID, skillID string, doc hCommon.SkillDocument, spec chatCommon.Spec, sink cCommon.ISink) (Plan, bool) {
	if sink == nil {
		sink = cCommon.NopSink{}
	}

	defaultPlanArgs := DefaultPlanArguments()
	defaultPlanArgs.KnownTools = hCommon.ConvertBasicTools2Map(doc.FrontMatter)

	plan, err := BuildPlanFromSkillSOPOutline(skillID, doc.FrontMatter.SOPOutLines, spec, defaultPlanArgs)
	if err != nil {
		logging.LogWarnF("plan mode:build plan failed,skill=%s,turn=%s,error=%s", skillID, turnID, err.Error())
		sink.OnNotice(cCommon.ChatNoticeIncomplete, "流程定义有误,已改为直接回答:"+err.Error())
		return Plan{}, false
	}

	// 计划必须对人可见,用户才能判断"是否该跑"
	verb := "计划就绪"
	if engine.chatConfig.Negotiation.RequireApproval {
		verb = "计划待确认"
	}
	sink.OnStatus("plan.preview", fmt.Sprintf(
		"%s:%s, %d 个步骤 / %d 个波次 / 预计 %d tokens", verb, spec.Brief(), plan.TaskCount, plan.WaveCount, plan.EstTokens))

	return plan, true
}

// SaveDraft 只落计划、不执行:审批通过前留一份可审计的快照
// 此函数不会产生重复行,真正执行时会被同一条记录覆盖
func (engine *PlanModeEngine) SaveDraft(ctx context.Context, turnID, sessionID string, plan Plan, spec chatCommon.Spec) error {
	if engine.storage == nil {
		return nil
	}

	if err := engine.storage.SavePlan(ctx, turnID, sessionID, plan, spec); err != nil {
		return err
	}

	return engine.storage.MarkPlanStatus(ctx, turnID, sessionID, StatusPending)
}

func (engine *PlanModeEngine) MarkPlanStatus(ctx context.Context, turnID string, sessionID string, status string) error {
	if engine.storage == nil {
		return nil
	}

	return engine.storage.MarkPlanStatus(ctx, turnID, sessionID, status)
}

// ============================= private functions & methods ===============================

func (engine *PlanModeEngine) loadResumeState(ctx context.Context, sessionId, turnID string, plan Plan) *PlanState {
	if engine.storage == nil {
		return nil
	}

	loaded, err := engine.storage.Load(ctx, sessionId, turnID)
	if err != nil {
		logging.LogWarnF("plan mode: load resume state failed,turnID=%s,error:%s", turnID, err.Error())
		return nil
	}

	if loaded == nil {
		return nil
	}

	if loaded.PlanDigest != GetFingerprintOfPlan(plan) {
		logging.LogWarnF("plan mode:fingerprint changed,turn=%s,drop resume state", turnID)
		return nil
	}

	return loaded.State
}

func (engine *PlanModeEngine) negotiateSpec(ctx context.Context, input NegotiationInput) NegotiationOutput {
	input.Sink.OnStatus("plan.negotiating", "正在确认需求...")

	retry := engine.chatConfig.Negotiation.ArgRetry
	if retry < 0 {
		retry = 0
	}
	if retry > 2 {
		retry = 2 // 最大重试 3 次
	}

	feedback := pCommon.BlankString
	for attempt := 0; attempt <= retry; attempt++ {
		result, err := engine.negotiator.Negotiate(ctx, NegotiationRequest{
			Provider:  input.Provider,
			Question:  input.Message,
			SkillId:   input.SkillID,
			SkillName: input.SkillDoc.FrontMatter.Name,
			SkillDesc: input.SkillDoc.FrontMatter.Description,
			SOPSteps:  GetSOPStepSummary(input.SkillDoc.FrontMatter.SOPOutLines),
			WorkRoot:  input.WorkRoot,
			Rules:     input.Rules,
			NegSpec:   input.NegSpec,
			Feedback:  feedback,
		})

		if err != nil {
			result = NegotiationResult{Decision: DecisionFailed, Reason: err.Error()}
		}

		switch result.Decision {
		case DecisionProceed:
			decoded, ok := input.NegSpec.ToSpec(result.Arg, input.SkillDoc.FrontMatter)
			if !ok {
				// proceed 却没有可用参数:按协商失败处理,不要带着空 spec 去建计划
				logging.LogWarnF("plan mode:proceed without spec,turn=%s,attempt=%d,type=%T", input.TurnID, attempt+1, result.Arg)
				feedback = "已判定为需要执行,但缺少必要参数;请用 " + IntentionToolStart + " 重新提交。"
				continue
			}
			issues := input.NegSpec.Validate(decoded, input.SkillDoc.FrontMatter)
			if len(issues) == 0 {
				projectDir := decoded.Fields()["project_dir"]
				if deniedPath, deniedErr := engine.checkReadPaths(input.NegSpec, decoded, input.Rules, input.WorkRoot); deniedErr != nil {
					feedback = "材料目录不可用:" + deniedErr.Error() + "\n请向用户确认目录,或改用可访问的路径。"
					logging.LogWarnF("plan mode: path denied,path=%s,turn=%s,dir=%s,error=%s", deniedPath, input.TurnID, projectDir, deniedErr.Error())
					continue
				}
				engine.reportDecision(ctx, input.TurnID, input.SkillID, string(DecisionProceed), result)
				return NegotiationOutput{Ready: true, Spec: decoded}
			}

			feedback = input.NegSpec.FormatIssues(issues)
			if strings.TrimSpace(feedback) == pCommon.BlankString {
				feedback = "参数不满足要求:" + strings.Join(issues, ",")
			}
			logging.LogWarnF("plan mode:spec rejected,turn=%s,attempt=%d,issues=%d", input.TurnID, attempt+1, len(issues))
			continue
		case DecisionNeedMoreInfo:
			engine.reportDecision(ctx, input.TurnID, input.SkillID, string(DecisionNeedMoreInfo), result)
			return NegotiationOutput{End: true, Answer: NeedMoreInfoText(result.Missing)}
		case DecisionNotApplicable:
			engine.reportDecision(ctx, input.TurnID, input.SkillID, string(DecisionNotApplicable), result)
			logging.LogInfoF("plan mode:not applicable,turn=%s,reason=%s", input.TurnID, result.Reason)
			return NegotiationOutput{} // 用户仅在问用法/概念：单轮聊天就能解决问题
		default:
			engine.reportDecision(ctx, input.TurnID, input.SkillID, string(DecisionFailed), result)
			logging.LogInfoF("plan mode:negotiate failed,turn=%s,reason=%s", input.TurnID, result.Reason)
			return NegotiationOutput{} // 协商失败(超时、未表态、上游错误):降级为单轮解决问题
		}
	}

	// 重试用尽:明确告诉用户缺了什么,而不是静默退回
	engine.reportDecision(ctx, input.TurnID, input.SkillID, string(DecisionValidateFailed), NegotiationResult{Decision: DecisionValidateFailed, Reason: feedback})
	return NegotiationOutput{End: true, Answer: "材料信息不完整,暂时无法执行。\n" + feedback}
}

func (engine *PlanModeEngine) forceSpec(ctx context.Context, input NegotiationInput) NegotiationOutput {
	candidate, issues := input.NegSpec.Default(input.SkillDoc.FrontMatter)
	if len(issues) > 0 {
		reason := input.NegSpec.FormatIssues(issues)
		engine.reportDecision(ctx, input.TurnID, input.SkillID, string(DecisionValidateFailed), NegotiationResult{Reason: reason})

		// force语义:不静默回退,明确报错
		userText := input.NegSpec.FormatIssuesForUser(issues)
		if strings.TrimSpace(userText) == pCommon.BlankString {
			userText = strings.Join(issues, "\n")
		}
		input.Sink.OnNotice(cCommon.ChatNoticeError, "force模式参数不可用(请检查[negotiation.projectDir]配置项:\n)"+userText)
		return NegotiationOutput{End: true, Answer: "force模式参数不可用(请检查[negotiation.projectDir]配置项):\n" + userText}
	}

	engine.reportDecision(ctx, input.TurnID, input.SkillID, string(DecisionProceed), NegotiationResult{Decision: "force mode"})
	return NegotiationOutput{Ready: true, Spec: candidate}
}

func (engine *PlanModeEngine) reportDecision(ctx context.Context, turnID, skillId, decision string, result NegotiationResult) {
	if engine.storage == nil {
		return
	}

	engine.storage.SaveDecision(ctx, dataobject.TIntentionDecision{
		TurnId:    turnID,
		SkillId:   skillId,
		Decision:  decision,
		Reason:    pCommon.TruncateRunes(result.Reason, 480, cCommon.TruncatedMarker),
		Missing:   pCommon.TruncateRunes(strings.Join(result.Missing, ","), 480, cCommon.TruncatedMarker),
		Rounds:    result.Rounds,
		ElapsedMs: result.ElapsedMs,
	})
}

func (engine *PlanModeEngine) loadSpecFields(ctx context.Context, sessionId, turnId string) map[string]string {
	if engine.storage == nil || strings.TrimSpace(turnId) == pCommon.BlankString {
		return nil
	}

	resumer, err := engine.storage.Load(ctx, sessionId, turnId)
	if err != nil {
		logging.LogWarnF("plan mode: load resumed spec failed,turn=%s,error=%s", turnId, err.Error())
		return nil
	}

	if resumer == nil {
		return nil
	}

	return resumer.SpecFields
}

func (engine *PlanModeEngine) checkReadPaths(negSpec chatCommon.INegotiationSpec, spec chatCommon.Spec, rules toolexecutor.ToolConfigRules, workRoot string) (string, error) {
	for _, readPath := range negSpec.ReadPaths(spec) {
		if strings.TrimSpace(readPath) == pCommon.BlankString {
			continue
		}

		if _, err := rules.CheckReadPath(readPath, workRoot); err != nil {
			return readPath, err
		}
	}

	return pCommon.BlankString, nil
}
