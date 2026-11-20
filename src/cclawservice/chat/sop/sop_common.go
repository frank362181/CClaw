/**
* 计划模型:声明式 SOP 展开后的可执行对象
*
* 与chat.TurnState的区别是:
* - TurnState 描述"一轮怎么答"(单轮执行单元的状态)
* - Plan 描述"一件事情分几步做"(多轮的编排)
*
* 设计思路:
* 1.Needs 用产物名，Depth 由构建期算好：执行器不需要再理解依赖语义
* 2.PlanTaskMerge 是第 4 种 kind,章节产物必须做确定性拼接
*  （不能交给 LLM、也不该"最后一节顺带产出"）
*	显式 merge 任务让计划可读、可测、可断点。它由 builder 生成，YAML 作者不需要写（仍写 produces: draft）
* 3.ReportSpec.Fields() 提供 ${spec.*} 的渲染源，避免到处 switch
**/

package sop

import (
	"context"
	"sort"
	"strings"
	"time"

	"my.favorite/cclawservice/biz/npa"
	hCommon "my.favorite/cclawservice/harness/common"
)

// 校验规则编号
const (
	RuleDupID          = "L1"
	RuleDanglingNeed   = "L2"
	RuleUnknownTool    = "L3"
	RuleMissingField   = "L4"
	RuleNoLLMStep      = "L5"
	RuleNoDeliverable  = "L6"
	RuleContractAbsent = "L7"
	RuleReviewerField  = "L8"
)

// PlanTaskKind 计划任务的执行方式
type PlanTaskKind int

const (
	PlanTaskTool  PlanTaskKind = iota + 1 // 调用一个已注册的工具(确定性)
	PlanTaskLLM                           // 起一轮 TurnRunner
	PlanTaskCheck                         // 跑机检查
	PlanTaskMerge                         // 确定性拼接(由builder生成,不在yaml里声明)
)

func (k PlanTaskKind) String() string {
	switch k {
	case PlanTaskTool:
		return "tool"
	case PlanTaskLLM:
		return "llm"
	case PlanTaskCheck:
		return "check"
	case PlanTaskMerge:
		return "merge"
	default:
		return "unknown"
	}
}

// 任务运行状态(与 t_turn_task.status 一致)
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusSkipped   = "skipped"  // M3′ 未实现的步骤(llm/check)
	StatusBlocked   = "blocked"  // 上游产物缺失,本步无法执行
	StatusCanceled  = "canceled" // 用户中断 / scope 取消
	StatusTimeout   = "timeout"
)

// PlanTask 一个可执行任务(按章节展开,一节一个任务)
type PlanTask struct {
	ID       string                  `json:"id"` // 计划内唯一:compose#basic_info
	ParentID string                  `json:"parentId,omitempty"`
	Section  string                  `json:"section,omitempty"` // 章节名
	Kind     PlanTaskKind            `json:"kind"`
	Action   string                  `json:"action"`          // tool名 / role名 / check集合名 / merge目标
	Needs    []string                `json:"needs,omitempty"` // 依赖的产物名
	Produces string                  `json:"produces,omitempty"`
	Args     map[string]string       `json:"args,omitempty"` // 已渲染 ${spec.*},保留 ${artifact.field}
	Checks   []string                `json:"checks,omitempty"`
	Gate     string                  `json:"gate,omitempty"`
	Policy   string                  `json:"policy,omitempty"`
	Contract hCommon.SOPStepContract `json:"contract,omitzero"`

	When        string `json:"when,omitempty"` // 非空 = 条件模版(不进初始波次)
	Conditional bool   `json:"conditional,omitempty"`

	Tries    int `json:"-"`
	MaxTries int `json:"maxTries,omitempty"`
	Depth    int `json:"depth"`
}

// PlanTaskState 单任务运行状态
type PlanTaskState struct {
	TaskID   string
	Status   string // pending / running / succeeded / failed / canceled / skipped
	Tries    int
	Artifact string
	GatePass bool
	Err      string
}

// Plan 展开后的完整计划
type Plan struct {
	SkillID string     `json:"skillId"`
	Tasks   []PlanTask `json:"tasks"`
	Waves   int        `json:"waves"` // 可调度任务的最大深度 + 1

	PlanPayload

	Conditional int `json:"conditional"` // 条件模版
}

// PlanRequest 表示一次执行任务的请求
type PlanRequest struct {
	TurnID    string
	SessionID string
	PlanMem   Plan
	Spec      npa.ReportSpec
	Sink      IPlanEventSink

	Tool ISopToolInvoker // 工具执行器

	ResumedState *PlanState // 断点续跑:非空则复用已有任务/产物
}

// PlanResult 一次计划执行的结果,与PlanRequest相对应
type PlanResult struct {
	State *PlanState
	Gaps  []Gap
	Waves int
}

// PlanArguments 计划展开的参数
type PlanArguments struct {
	MaxTasks           int
	ChapterTokens      int
	ToolReplyTokens    int
	RoundP95           time.Duration
	SequentialSections bool                // 章节串行(结论引用前文)
	KnownTools         map[string]struct{} // 必填:SKILL.md中outline含tool必须给出工具名称
}

// PlanPayload  计划规模:只服务预算与canDelegate
type PlanPayload struct {
	TaskCount  int           `json:"taskCount"`
	WaveCount  int           `json:"waveCount"`
	EstTokens  int64         `json:"estTokens"`
	EstLatency time.Duration `json:"estLatency"` // 纳秒
}

// Severity 缺口级别
type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarning
	SeverityFail
)

func (s Severity) String() string {
	switch s {
	case SeverityFail:
		return "fail"
	case SeverityWarning:
		return "warning"
	default:
		return "info"
	}
}

func ParseSeverity(s string) Severity {
	switch strings.ToLower(s) {
	case "info":
		return SeverityInfo
	case "warning":
		return SeverityWarning
	case "fail":
		return SeverityFail
	default:
		return SeverityInfo
	}
}

const (
	CodeTaskGateUnknown       = "gate_unknown"
	CodeTaskGateFailed        = "gate_failed"
	CodeTaskGateToolFailed    = "tool_failed"
	CodeTaskGateArgResolve    = "arg_resolve"
	CodeTaskGateExtractFailed = "artifact_extract"
	CodeTaskGateTimeout       = "task_timeout"
	CodeTaskGateUnImplemented = "not_implemented"
)

// Gap 每步task执行完成后校验发现的缺口,
// 能自动补的带Fix,不能自动补的进报告标注
type Gap struct {
	Code     string
	TaskID   string
	Section  string
	Detail   string
	Severity Severity
	Fix      *PlanTask
}

// Artifact 产物引用(name -> 可解析的定位)
type Artifact struct {
	Name         string            // 产物名
	ArtifactKind string            // file | dir | json | text
	AbsolutePath string            // 绝对路径
	Digest       string            // 内容指纹
	Fields       map[string]string // 常用字段预取(path|file|pages|files_need_ocr...)
}

// PlanState 一轮计划的运行状态
type PlanState struct {
	TurnID    string
	SessionID string
	SkillID   string

	Spec npa.ReportSpec

	Wave       int
	Round      int
	TaskStates map[string]PlanTaskState
	Artifacts  map[string]Artifact
	Gaps       []Gap
}

func NewPlanState(turnID, sessionId, skillId string, spec npa.ReportSpec) *PlanState {
	return &PlanState{
		TurnID:     turnID,
		SessionID:  sessionId,
		SkillID:    skillId,
		Spec:       spec,
		TaskStates: make(map[string]PlanTaskState),
		Artifacts:  make(map[string]Artifact),
	}
}

// SortedArtifacts 稳定排序的产物名
func (p PlanState) SortedArtifacts() []string {
	names := make([]string, 0, len(p.Artifacts))
	for name := range p.Artifacts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ISopToolInvoker 执行一次工具调用,返回原始的JSON文本
type ISopToolInvoker interface {
	Invoke(ctx context.Context, toolName string, args map[string]string) (string, error)
}

// PlanResumer 从库里读回一轮计划
type PlanResumer struct {
	PlanDigest string         // 落库时的计划指纹,与重新展开时的结果对比
	PlanJson   string         // 落库时的plan_json,指纹不匹配时用于输出差异
	Spec       npa.ReportSpec //落库时的需求规格
	State      *PlanState     // 任务状态 + 产物 + 缺口
}

type IPlanResume interface {
	Load(ctx context.Context, turnID string) (*PlanResumer, error)
}

type NopPlanResumer struct{}

func (NopPlanResumer) Load(ctx context.Context, turnID string) (*PlanResumer, error) {
	return nil, nil
}

// IPlanStorage 计划的存储器,可以为空,方便于测试
type IPlanStorage interface {
	SavePlan(ctx context.Context, turnID, sessionID string, plan Plan, spec npa.ReportSpec) error
	SaveTask(ctx context.Context, turnID string, planTask PlanTask, state PlanTaskState, inputDigest string) error
	SaveGaps(ctx context.Context, turnID string, round int, gaps []Gap) error
	SaveArtifact(ctx context.Context, turnID string, artifact Artifact) error
}

type NopPlanStorage struct{}

func NewNoPlanStorage() *NopPlanStorage {
	return &NopPlanStorage{}
}

func (NopPlanStorage) SavePlan(ctx context.Context, turnID, sessionID string, plan Plan, spec npa.ReportSpec) error {
	return nil
}

func (NopPlanStorage) SaveTask(ctx context.Context, turnID string, planTask PlanTask, state PlanTaskState, inputDigest string) error {
	return nil
}

func (NopPlanStorage) SaveGaps(ctx context.Context, turnID string, round int, gaps []Gap) error {
	return nil
}

func (NopPlanStorage) SaveArtifact(ctx context.Context, turnID string, artifact Artifact) error {
	return nil
}

// IPlanEventSink 执行事件,与chat.ISink适配
type IPlanEventSink interface {
	OnTask(task PlanTask, state PlanTaskState)
	OnGate(task PlanTask, gaps []Gap)
}

type NopPlanEventSink struct{}

func NewNopPlanEventSink() *NopPlanEventSink {
	return &NopPlanEventSink{}
}

func (p *NopPlanEventSink) OnTask(task PlanTask, state PlanTaskState) {}
func (p *NopPlanEventSink) OnGate(task PlanTask, gaps []Gap)          {}

// ArtifactExtractor 把工具的原始输出抽取出来形成产物
type ArtifactExtractor func(planTask PlanTask, rawData string) (Artifact, error)
