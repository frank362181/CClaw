/**
* 执行编排相关信息表,用来存放从SKILL.md中解析出来的最终形成的执行计划信息
*
* 设计要点:
* - t_plan 是 **快照**,不是执行输入,执行期永远用内存中的sop.Plan
*   resume时冲新从SKILL.md展开,再用plan_digest与快照对比,一致才复用任务状态
* - fields_json不能省:它是${artifact.field}的唯一渲染来源
**/

package dataobject

// TPlan 计划,包含预算估算
type TPlan struct {
	Id           int64  `gorm:"column:id;primaryKey" json:"Id"`
	TurnId       string `gorm:"column:turn_id" json:"TurnId"`
	SessionId    string `gorm:"column:session_id" json:"SessionId"`
	SkillId      string `gorm:"column:skill_id" json:"SkillId"`
	SpecJson     string `gorm:"column:spec_json" json:"SpecJson"`       // ReportSpec 快照
	PlattenJson  string `gorm:"column:platter_json" json:"PlatterJson"` // 压扁后的JSON字符串
	JsonDigest   string `gorm:"column:json_digest" json:"JsonDigest"`   // resume 一致性校验
	Waves        int    `gorm:"column:waves" json:"Waves"`
	TaskCount    int    `gorm:"column:task_count" json:"TaskCount"`
	EstTokens    int64  `gorm:"column:est_tokens" json:"EstTokens"`
	EstLatencyMs int64  `gorm:"column:est_latency_ms" json:"EstLatencyMs"`
	LintOk       bool   `gorm:"column:lint_ok" json:"LintOk"`
	Status       string `gorm:"column:status" json:"Status"`
	CreatedAt    int64  `gorm:"column:created_at" json:"CreatedAt"`
	UpdatedAt    int64  `gorm:"column:updated_at" json:"UpdatedAt"`
}

func (TPlan) TableName() string { return "t_plan" }

// TPlanTask 表示计划中的一个任务
type TPlanTask struct {
	Id          int64  `gorm:"column:id;primaryKey" json:"Id"`
	TurnId      string `gorm:"column:turn_id;uniqueIndex:uk_turn_task,priority 1" json:"TurnId"`
	TaskId      string `gorm:"column:task_id;uniqueIndex:uk_turn_task,priority 2" json:"TaskId"`
	Kind        string `gorm:"column:kind" json:"Kind"`
	Action      string `gorm:"column:action" json:"Action"`
	Section     string `gorm:"column:section" json:"Section"`
	Status      string `gorm:"column:status" json:"Status"`
	Tries       int    `gorm:"column:tries" json:"Tries"`
	StartedAt   int64  `gorm:"column:started_at" json:"StartedAt"`
	EndedAt     int64  `gorm:"column:ended_at" json:"EndedAt"`
	InputDigest string `gorm:"column:input_digest" json:"InputDigest"`
	OutputName  string `gorm:"column:output_name" json:"OutputName"`
	GatePass    bool   `gorm:"column:gate_pass" json:"GatePass"`
	Err         string `gorm:"column:err" json:"Err"`
	UpdatedAt   int64  `gorm:"column:updated_at" json:"UpdatedAt"`
}

func (TPlanTask) TableName() string { return "t_plan_task" }

// TPlanGap 门禁/机检发现的缺口
type TPlanGap struct {
	Id        int64  `gorm:"column:id;primaryKey" json:"Id"`
	TurnId    string `gorm:"column:turn_id" json:"TurnId"`
	Round     int    `gorm:"column:round" json:"Round"`
	Code      string `gorm:"column:code" json:"Code"`
	TaskId    string `gorm:"column:task_id" json:"TaskId"`
	Section   string `gorm:"column:section" json:"Section"`
	Severity  string `gorm:"column:severity" json:"Severity"`
	Detail    string `gorm:"column:detail" json:"Detail"`
	Fixed     bool   `gorm:"column:fixed" json:"Fixed"`
	CreatedAt int64  `gorm:"column:created_at" json:"CreatedAt"`
}

func (TPlanGap) TableName() string { return "t_plan_gap" }

// TArtifact 产物(只增;同名多 rev)
type TArtifact struct {
	Id         int64  `gorm:"column:id;primaryKey" json:"Id"`
	TurnId     string `gorm:"column:turn_id" json:"TurnId"`
	Name       string `gorm:"column:name" json:"Name"`
	Rev        int    `gorm:"column:rev" json:"Rev"`
	Kind       string `gorm:"column:kind" json:"Kind"`
	Ref        string `gorm:"column:ref" json:"Ref"`
	Digest     string `gorm:"column:digest" json:"Digest"`
	Size       int64  `gorm:"column:size" json:"Size"`
	FieldsJson string `gorm:"column:fields_json" json:"FieldsJson"` // map[string]string
	CreatedAt  int64  `gorm:"column:created_at" json:"CreatedAt"`
}

func (TArtifact) TableName() string { return "t_artifact" }

// TIntentionDecision 问题意图识别的结果
type TIntentionDecision struct {
	Id        int64  `gorm:"column:id;primaryKey" json:"Id"`
	TurnId    string `gorm:"column:turn_id" json:"TurnId"`
	SkillId   string `gorm:"column:skill_id" json:"SkillId"`
	Decision  string `gorm:"column:decision" json:"Decision"`
	Reason    string `gorm:"column:reason" json:"Reason"`
	Missing   string `gorm:"column:missing" json:"Missing"`
	Rounds    int    `gorm:"column:rounds" json:"Rounds"`
	ElapsedMs int64  `gorm:"column:elapsed_ms" json:"ElapsedMs"`
	CreatedAt int64  `gorm:"column:created_at" json:"CreatedAt"`
}

func (TIntentionDecision) TableName() string { return "t_intention_decision" }
