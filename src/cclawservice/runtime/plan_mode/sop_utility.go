/**
* 编排层所需的工具函数
**/

package plan_mode

import (
	"fmt"
	"strings"

	bizCommon "my.favorite/cclawservice/biz/common"
	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/common/llmproto"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/common/hash"
)

// plan_json的初始版本,变更时 + 1
const planSnapshotVersion = 1

type planWriteFormat struct {
	Version int `json:"version"`
	Plan
}

func MarshalPlanToJson(plan Plan) (string, error) {
	data, err := pCommon.MarshalJson(planWriteFormat{
		Version: planSnapshotVersion,
		Plan:    plan,
	})

	if err != nil {
		return pCommon.BlankString, fmt.Errorf("plan digest: marshal failed: %w", err)
	}

	return data, nil
}

func UnmarshalPlanFromJson(text string) (Plan, error) {
	var writeFormat planWriteFormat
	if err := pCommon.UnmarshalJson(text, &writeFormat); err != nil {
		return Plan{}, fmt.Errorf("plan digest: unmarshal failed: %w", err)
	}

	if writeFormat.Version != planSnapshotVersion {
		return Plan{}, fmt.Errorf("plan digest: version mismatch, want %d, got %d", planSnapshotVersion, writeFormat.Version)
	}

	return writeFormat.Plan, nil
}

func GetFingerprintOfPlan(plan Plan) string {
	text, err := MarshalPlanToJson(plan)
	if err != nil {
		return pCommon.BlankString
	}

	digest, err := hash.SHA256(text)
	if err != nil {
		return pCommon.BlankString
	}

	return digest
}

func IsTerminal(status string) bool {
	switch status {
	case StatusSucceeded, StatusFailed, StatusSkipped, StatusBlocked, StatusCanceled, StatusTimeout:
		return true
	default:
		return false
	}
}

// GetSOPChatResultSummary 面向用户的执行摘要
// 分母"计划任务总数"而不是"已进 TaskStates的数量"
// 否则波次中断会把共N步报成只执行 n 步
func GetSOPChatResultSummary(result PlanResult, total int) string {
	if result.State == nil {
		return pCommon.BlankString
	}

	count := make(map[string]int, 8)
	for _, st := range result.State.TaskStates {
		count[st.Status]++
	}
	lines := make([]string, 0, 6)
	lines = append(lines, fmt.Sprintf("计划 %d 个步骤(成功 %d,失败 %d,阻塞 %d,跳过 %d,取消 %d)。",
		total,
		count[StatusSucceeded],
		count[StatusFailed]+count[StatusTimeout],
		count[StatusBlocked],
		count[StatusSkipped],
		count[StatusCanceled],
	))
	if names := result.State.SortedArtifacts(); len(names) > 0 {
		lines = append(lines, "产出:"+strings.Join(names, "、"))
	}

	fails := make([]string, 0, len(result.Gaps))
	for _, gap := range result.Gaps {
		if gap.Severity == SeverityFail {
			fails = append(fails, gap.Code)
		}
	}

	if len(fails) > 0 {
		lines = append(lines, "未通过的校验:"+strings.Join(fails, "、"))
	}

	return strings.Join(lines, "\n")
}

func SpecFieldsChanged(before, after bizCommon.ISpec) bool {
	if before == nil || after == nil {
		return (before == nil) != (after == nil)
	}

	beforeFields, afterFields := before.Fields(), after.Fields()
	if len(beforeFields) != len(afterFields) {
		return true
	}

	for key, value := range beforeFields {
		if afterFields[key] != value {
			return true
		}
	}

	return false
}

// 将工具调用列表压成扁的string,方便日志输出
func DescribeToolCalls(calls []llmproto.ToolCall) string {
	if len(calls) == 0 {
		return pCommon.BlankString
	}

	var builder strings.Builder
	for i, call := range calls {
		if i > 0 {
			builder.WriteString(";")
		}

		builder.WriteString(call.Function.Name)
		builder.WriteString("(")
		builder.WriteString(pCommon.TruncateRunes(call.Function.Arguments, 512, cCommon.TruncatedMarker))
		builder.WriteString(")")
	}

	return builder.String()
}
