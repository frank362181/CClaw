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

	"my.favorite/cclawservice/biz/npa"
	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/config"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/model/dataobject"
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

	var outcome NegotiationOutput
	if engine.chatConfig.Chat.Mode == cCommon.PlanModeForce {
		outcome = engine.forceSpec(ctx, input)
	} else {
		outcome = engine.negotiateSpec(ctx, input)
	}

	if !outcome.Ready {
		return outcome
	}

	outcome.Spec.ArchiveNo = engine.getArchiveNo(ctx, input.SessionID, input.TurnID, outcome.Spec.ProjectDir, outcome.Spec.ArchiveNo)
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
		Answer: GetSOPChatResultSummary(planResult),
		State:  planResult.State,
		Gaps:   planResult.Gaps,
		Waves:  planResult.Waves,
	}, nil
}

// PreviewPlan 把SOP大纲展开成计划,并向用户播报预览
// 展开失败不会回退为错误,而是降级为单轮(返回 ok = false)
func (engine *PlanModeEngine) PreviewPlan(turnID, skillID string, doc hCommon.SkillDocument, spec npa.ReportSpec, sink cCommon.ISink) (Plan, bool) {
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
	sink.OnStatus("plan.preview", fmt.Sprintf(
		"计划就绪:目录=%s,档案号=%s, %d 个步骤 / %d 个波次 / 预计 %d tokens",
		spec.ProjectDir, cCommon.FallbackText(spec.ArchiveNo, "(空)"), plan.TaskCount, plan.WaveCount, plan.EstTokens))

	return plan, true
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
			NegSpec:   npa.NewNPANegotiationSpec(input.SkillDoc.FrontMatter),
			Feedback:  feedback,
		})

		if err != nil {
			result = NegotiationResult{Decision: DecisionFailed, Reason: err.Error()}
		}

		switch result.Decision {
		case DecisionProceed:
			decoded, ok := result.Arg.(npa.ReportSpec)
			if !ok {
				// proceed 却没有可用参数:按协商失败处理,不要带着空 spec 去建计划
				logging.LogWarnF("plan mode:proceed without spec,turn=%s,attempt=%d,type=%T", input.TurnID, attempt+1, result.Arg)
				feedback = "已判定为需要执行,但缺少必要参数;请用 " + IntentionToolStart + " 重新提交。"
				continue
			}
			candidate := npa.NormalizeReportSpec(decoded, input.SkillDoc.FrontMatter)
			issues := npa.ValidateReportSpec(candidate, input.SkillDoc.FrontMatter)
			if len(issues) == 0 {
				engine.reportDecision(ctx, input.TurnID, input.SkillID, string(DecisionProceed), result)
				return NegotiationOutput{Ready: true, Spec: candidate}
			}

			feedback = npa.FormatSpecIssues(issues)
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
	dftProjectDir := engine.chatConfig.Negotiation.ProjectDir
	candidate := npa.NormalizeReportSpec(npa.ReportSpec{
		ProjectDir: dftProjectDir,
		ArchiveNo:  "",
		ReportKind: npa.DefaultReportKind,
		OCR:        npa.DefaultOCRMode,
	}, input.SkillDoc.FrontMatter)

	if issues := npa.ValidateReportSpec(candidate, input.SkillDoc.FrontMatter); len(issues) > 0 {
		engine.reportDecision(ctx, input.TurnID, input.SkillID, string(DecisionValidateFailed), NegotiationResult{Reason: npa.FormatSpecIssues(issues)})
		// force语义:不静默回退,明确报错
		input.Sink.OnNotice(cCommon.ChatNoticeError, "force模式参数不可用(请检查[negotiation.projectDir]配置项:\n)"+
			npa.FormatSpecIssuesForUser(issues))

		return NegotiationOutput{End: true, Answer: "force模式参数不可用(请检查[negotiation.projectDir]配置项):\n" + npa.FormatSpecIssuesForUser(issues)}
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

// 产生一个新的ArchiveNo
func (engine *PlanModeEngine) getArchiveNo(ctx context.Context, sessionId, turnID string, projectDir string, archiveNo string) string {
	if no := strings.TrimSpace(archiveNo); no != "" {
		return no
	}

	if strings.TrimSpace(turnID) != pCommon.BlankString {
		resumer, err := engine.storage.Load(ctx, sessionId, turnID)
		if err != nil {
			logging.LogWarnF("plan mode:load resumed spec failed,turn=%s,err=%s", turnID, err.Error())
		} else if resumer != nil {
			if no := strings.TrimSpace(resumer.Spec.ArchiveNo); no != pCommon.BlankString {
				return no
			}
		}
	}

	return npa.GenerateDocumentNo(projectDir, "NPA")
}
