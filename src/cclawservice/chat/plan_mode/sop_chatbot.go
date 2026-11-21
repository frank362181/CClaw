/**
* 目的:
*   按Depth分波执行, tool步真跑、merger步拼接、check/llm步跳过;每步必过门禁
*
* 计划执行器,依赖于ToolInvocation接口,原因:
* - 工具调用步骤需要"工具名 + 参数  -> 原始输出",不需要Prepare/路由/预算
* - 调用时在service层 实现 ToolInvoker(内部用 TurnState.NewToolEnv)即可
**/

package plan_mode

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	cCommon "my.favorite/cclawservice/common"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/common/hash"
	"my.favorite/platformutility/concurrency"
)

// Extractors 按照工具名索引的抽取器
type Extractors struct {
	extractorByTool map[string]ArtifactExtractor
}

func NewExtractors() *Extractors {
	return &Extractors{
		extractorByTool: make(map[string]ArtifactExtractor, 8),
	}
}

func (e *Extractors) RegisterExtractor(toolName string, extractor ArtifactExtractor) {
	toolName = strings.ToLower(toolName)
	if len(toolName) == 0 {
		return
	}

	e.extractorByTool[toolName] = extractor
}

func (e *Extractors) Extract(planTask PlanTask, rawData string) (Artifact, error) {
	extractor, ok := e.extractorByTool[strings.ToLower(planTask.Action)]
	if !ok {
		return Artifact{}, fmt.Errorf("executor:tool %s not found in extractors", planTask.Action)
	}

	return extractor(planTask, rawData)
}

type PlanChatBotParams struct {
	ConGate concurrency.IConcurrencyController
	Sink    cCommon.ISink

	Extractors *Extractors
	Barriers   *BarrierRepository
	PlanStorge IPlanStorage
}

type PlanChatBotRunner struct {
	conGate concurrency.IConcurrencyController
	sink    cCommon.ISink

	extractors  *Extractors
	barriers    *BarrierRepository
	planStorage IPlanStorage

	maxRoutines int           // 单波并发上限
	tasksWait   time.Duration // 单波收敛上限
}

// NewPlanChatBotRunner PlanChatBotRunner 每次都新建,避免并发情况下后一次的调用将前一次的给覆盖掉
func NewPlanChatBotRunner(chatBotParams PlanChatBotParams) *PlanChatBotRunner {
	planExecutor := &PlanChatBotRunner{
		conGate: chatBotParams.ConGate,
		sink:    chatBotParams.Sink,

		extractors:  chatBotParams.Extractors,
		barriers:    chatBotParams.Barriers,
		planStorage: chatBotParams.PlanStorge,

		maxRoutines: 8,
		tasksWait:   time.Duration(180) * time.Second,
	}

	if planExecutor.extractors == nil {
		planExecutor.extractors = NewExtractors()
	}
	if planExecutor.barriers == nil {
		planExecutor.barriers = NewBarrierRepository()
	}
	if planExecutor.planStorage == nil {
		planExecutor.planStorage = NewNoPlanStorage() //Plan存储的兜底
	}

	return planExecutor
}

// Run 按波次执行计划
func (e *PlanChatBotRunner) Run(ctx context.Context, req PlanRequest) (PlanResult, error) {
	state := req.ResumedState
	if state == nil {
		state = NewPlanState(req.TurnID, req.SessionID, req.PlanMem.SkillID, req.Spec)
	} else {
		state.TurnID, state.SessionID, state.Spec = req.TurnID, req.SessionID, req.Spec
	}

	artifactRepo := NewArtifactMgr()
	resumeArtifact(artifactRepo, state)

	if err := e.planStorage.SavePlan(ctx, req.TurnID, req.SessionID, req.PlanMem, req.Spec); err != nil {
		return PlanResult{}, fmt.Errorf("executor:failed to save:%w", err)
	}

	stateMu := &sync.Mutex{}
	aborted := false

	for wave := 0; wave < req.PlanMem.Waves && !aborted; wave++ {
		waveTasks := getSchedulableAt(req.PlanMem.Tasks, wave, state, stateMu)
		if len(waveTasks) == 0 {
			continue
		}

		scope := concurrency.GetTaskManager().NewScope(fmt.Sprintf("%s:wave-%d", req.TurnID, wave), e.maxRoutines)
		dispatched := make([]PlanTask, 0, len(waveTasks))
		for _, task := range waveTasks {
			current := task

			// 上游产物缺失 -> 本步 blocked,且不在派发(否则依赖约束形同虚设)
			if missing := missingNeeds(current, artifactRepo); len(missing) > 0 {
				e.mark(stateMu, state, current, PlanTaskState{
					TaskID: current.ID,
					Status: StatusBlocked,
					Err:    fmt.Sprintf("wait for the outcome from previous:%s", strings.Join(missing, ",")),
				}, req, artifactRepo)

				e.addGaps(stateMu, state, []Gap{{
					Code:     CodeTaskGateFailed,
					TaskID:   current.ID,
					Section:  current.Section,
					Detail:   "upstream artifacts missing:" + strings.Join(missing, ","),
					Severity: SeverityFail,
				}}, req)
				continue
			}

			if err := scope.Go(ctx, concurrency.RoutineSpec{
				Name: "sop-task-" + current.ID,
				Fn: func(runCtx context.Context) error {
					e.runTask(runCtx, req, current, state, stateMu, artifactRepo)
					return nil
				},
			}); err != nil {
				e.mark(stateMu, state, current, PlanTaskState{
					TaskID: current.ID,
					Status: StatusFailed,
					Err:    "dispatch:" + err.Error(),
				}, req, artifactRepo)
				continue
			}

			dispatched = append(dispatched, current)
		}

		waitCtx, cancel := context.WithTimeout(context.Background(), e.tasksWait)
		waitErr := scope.Wait(waitCtx)
		cancel()
		scope.Cancel()
		if waitErr != nil {
			//波次没收敛:把仍然在跑的任务标记超时,并且停止后续波次(依赖链已断)
			aborted = e.markNotFinish(dispatched, waitErr, state, stateMu, req, artifactRepo)
		}
	}

	return PlanResult{
		State: state,
		Gaps:  state.Gaps,
		Waves: req.PlanMem.Waves,
	}, nil
}

// =========================== inner functions & methods ==========================

func (e *PlanChatBotRunner) runTask(ctx context.Context, req PlanRequest, planTask PlanTask,
	planState *PlanState, stateMu *sync.Mutex, artifactRepo *ArtifactMgr) {

	switch planTask.Kind {
	case PlanTaskTool:
		e.runToolTask(ctx, req, planTask, planState, stateMu, artifactRepo)
	case PlanTaskMerge:
		e.runMergeTask(planTask, planState, stateMu, artifactRepo, req)
	case PlanTaskCheck, PlanTaskLLM:
		e.mark(stateMu, planState, planTask, PlanTaskState{
			TaskID: planTask.ID,
			Status: StatusSkipped,
			Err:    fmt.Sprintf("unsupported task kind:%s", planTask.Kind.String()),
		}, req, artifactRepo)

		kind := planTask.Kind.String()
		e.addGaps(stateMu, planState, []Gap{{
			Code:     CodeTaskGateUnImplemented,
			TaskID:   planTask.ID,
			Section:  planTask.Section,
			Detail:   fmt.Sprintf("unimplemented task kind:%s", kind),
			Severity: SeverityFail,
		}}, req)
	default:
		e.mark(stateMu, planState, planTask, PlanTaskState{
			TaskID: planTask.ID,
			Status: StatusFailed,
			Err:    "unknown task kind",
		}, req, artifactRepo)
	}
}

func (e *PlanChatBotRunner) runToolTask(ctx context.Context, req PlanRequest, planTask PlanTask, planState *PlanState, stateMu *sync.Mutex, artifactsRepo *ArtifactMgr) {
	// 幂等:同轮已成功直接跳过
	if st, ok := planState.TaskStates[planTask.ID]; ok && st.Status == StatusSucceeded {
		return
	}

	args, err := artifactsRepo.RenderArgs(planTask.Args)
	if err != nil {
		e.mark(stateMu, planState, planTask, PlanTaskState{
			TaskID: planTask.ID,
			Status: StatusFailed,
			Err:    err.Error(),
		}, req, artifactsRepo)
		e.addGaps(stateMu, planState, []Gap{{
			Code:     CodeTaskGateArgResolve,
			TaskID:   planTask.ID,
			Section:  planTask.Section,
			Detail:   err.Error(),
			Severity: SeverityFail,
		}}, req)
		return
	}
	if req.Tool == nil {
		e.mark(stateMu, planState, planTask, PlanTaskState{
			TaskID: planTask.ID,
			Status: StatusBlocked,
			Err:    fmt.Sprintf("no tool found in task:%s", planTask.ID),
		}, req, artifactsRepo)
		return
	}

	raw, err := req.Tool.Invoke(ctx, planTask.Action, args)
	if err != nil {
		e.mark(stateMu, planState, planTask, PlanTaskState{
			TaskID: planTask.ID,
			Status: StatusFailed,
			Err:    err.Error(),
		}, req, artifactsRepo)

		e.addGaps(stateMu, planState, []Gap{{
			Code:     CodeTaskGateToolFailed,
			TaskID:   planTask.ID,
			Section:  planTask.Section,
			Detail:   err.Error(),
			Severity: SeverityFail,
		}}, req)

		return
	}

	artifact, err := e.extractors.Extract(planTask, raw)
	if err != nil {
		e.mark(stateMu, planState, planTask, PlanTaskState{
			TaskID: planTask.ID,
			Status: StatusFailed,
			Err:    err.Error(),
		}, req, artifactsRepo)
		e.addGaps(stateMu, planState, []Gap{{
			Code:     CodeTaskGateExtractFailed,
			TaskID:   planTask.ID,
			Section:  planTask.Section,
			Detail:   err.Error(),
			Severity: SeverityFail,
		}}, req)

		return
	}

	artifactsRepo.Put(artifact)

	// 任务执行结果检查:每步执行完立即判断(纯函数,可在任务写成内跑)
	gaps := e.barriers.Evaluate(TaskBarrierInput{
		Task:   planTask,
		Output: artifact,
		Lookup: artifactsRepo.Get,
		Spec:   req.Spec,
	})

	status := StatusSucceeded
	for _, gap := range gaps {
		if gap.Severity == SeverityFail {
			status = StatusFailed
			break
		}
	}

	stateMu.Lock()
	planState.Artifacts[artifact.Name] = artifact
	stateMu.Unlock()

	e.mark(stateMu, planState, planTask, PlanTaskState{
		TaskID:   planTask.ID,
		Status:   status,
		Artifact: artifact.Name,
		GatePass: status == StatusSucceeded,
	}, req, artifactsRepo)
	if len(gaps) > 0 {
		e.addGaps(stateMu, planState, gaps, req)
	}

	_ = e.planStorage.SaveArtifact(ctx, req.TurnID, artifact)
}

func (e *PlanChatBotRunner) runMergeTask(planTask PlanTask, planState *PlanState, stateMu *sync.Mutex, artifactRepo *ArtifactMgr, req PlanRequest) {
	parts := make([]string, 0, len(planTask.Needs))
	for _, need := range planTask.Needs {
		text, err := artifactRepo.RenderArg("${" + need + ".text}")
		if err != nil {
			e.mark(stateMu, planState, planTask, PlanTaskState{
				TaskID: planTask.ID,
				Status: StatusBlocked,
				Err:    err.Error(),
			}, req, artifactRepo)
			return
		}

		parts = append(parts, text)
	}

	artifact := Artifact{
		Name:         planTask.Produces,
		ArtifactKind: "text",
		AbsolutePath: strings.Join(parts, "\n\n"),
		Fields:       map[string]string{"parts": fmt.Sprintf("%d", len(parts))},
	}
	artifactRepo.Put(artifact)

	stateMu.Lock()
	planState.Artifacts[artifact.Name] = artifact
	stateMu.Unlock()

	e.mark(stateMu, planState, planTask, PlanTaskState{
		TaskID:   planTask.ID,
		Status:   StatusSucceeded,
		Artifact: artifact.Name,
		GatePass: true,
	}, req, artifactRepo)
}

func (e *PlanChatBotRunner) mark(stateMu *sync.Mutex, planState *PlanState, task PlanTask, taskState PlanTaskState, req PlanRequest,
	artifacts *ArtifactMgr) {
	stateMu.Lock()
	planState.TaskStates[task.ID] = taskState
	stateMu.Unlock()

	req.Sink.OnTask(task, taskState)

	_ = e.planStorage.SaveTask(context.Background(), req.TurnID, task, taskState, getToolDigest(task, artifacts))
}

func (e *PlanChatBotRunner) addGaps(stateMu *sync.Mutex, planState *PlanState, gaps []Gap, req PlanRequest) {
	if len(gaps) == 0 {
		return
	}

	stateMu.Lock()
	planState.Gaps = append(planState.Gaps, gaps...)
	round := planState.Round
	stateMu.Unlock()

	req.Sink.OnGate(PlanTask{ID: gaps[0].TaskID}, gaps)

	_ = e.planStorage.SaveGaps(context.Background(), req.TurnID, round, gaps)
}

// 波次超时:把未终态任务标记为超时 timeout,返回"是否应终止任务"
func (e *PlanChatBotRunner) markNotFinish(planTasks []PlanTask, waitErr error, planState *PlanState, stateMu *sync.Mutex,
	req PlanRequest, artifacts *ArtifactMgr) bool {
	for _, task := range planTasks {
		stateMu.Lock()
		curState, ok := planState.TaskStates[task.ID]
		stateMu.Unlock()

		if ok && IsTerminal(curState.Status) {
			continue
		}

		e.mark(stateMu, planState, task, PlanTaskState{
			TaskID: task.ID,
			Status: StatusTimeout,
			Err:    waitErr.Error(),
		}, req, artifacts)

		e.addGaps(stateMu, planState, []Gap{{
			Code:     CodeTaskGateTimeout,
			TaskID:   task.ID,
			Section:  task.Section,
			Detail:   "wave unfinished in the timeout:" + waitErr.Error(),
			Severity: SeverityFail,
		}}, req)
	}

	return true // 依赖链已断,不再进入后续波次
}

// 把断点续跑带回来的产物装进repo
func resumeArtifact(repo *ArtifactMgr, state *PlanState) {
	if state == nil {
		return
	}

	for _, artifact := range state.Artifacts {
		repo.Put(artifact)
	}
}

// 取某波次里"尚未成功"的可调度任务
func getSchedulableAt(tasks []PlanTask, wave int, state *PlanState, stateMu *sync.Mutex) []PlanTask {
	out := make([]PlanTask, 0, 4)

	for _, task := range tasks {
		if task.Conditional || task.Depth != wave {
			continue
		}

		stateMu.Lock()
		current, ok := state.TaskStates[task.ID]
		stateMu.Unlock()
		if ok && current.Status == StatusSucceeded {
			continue // 已成功,不重跑
		}

		out = append(out, task)
	}

	return out
}

// 返回尚未产出的依赖产物
func missingNeeds(task PlanTask, artifactRepo *ArtifactMgr) []string {
	missing := make([]string, 0, len(task.Needs))

	for _, need := range task.Needs {
		if _, ok := artifactRepo.Get(need); !ok {
			missing = append(missing, need)
		}
	}

	return missing
}

func getToolDigest(task PlanTask, repo *ArtifactMgr) string {
	if task.Kind != PlanTaskTool {
		return ""
	}

	args, err := repo.RenderArgs(task.Args)
	if err != nil {
		return pCommon.BlankString
	}

	result, _ := hash.SHA256(task.Action + "|" + pCommon.JoinStringFromMap(args))
	return result
}
