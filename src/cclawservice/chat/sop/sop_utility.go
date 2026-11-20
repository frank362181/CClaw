/**
* 编排层所需的工具函数
**/

package sop

import (
	"fmt"
	"strings"

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

func GetSOPChatResultSummary(result PlanResult) string {
	if result.State == nil {
		return pCommon.BlankString
	}

	succeeded, failed := 0, 0
	for _, state := range result.State.TaskStates {
		switch state.Status {
		case StatusSucceeded:
			succeeded++
		case StatusFailed, StatusTimeout:
			failed++
		}
	}

	lines := make([]string, 0, 4)
	lines = append(lines, fmt.Sprintf("已执行 %d 个步骤(成功 %d,失败 %d)。", len(result.State.TaskStates), succeeded, failed))

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
