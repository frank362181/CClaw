/*
* 允许工具自定义沙箱策略
*	未实现该接口的,则使用默认的沙箱策略
**/

package toolexecutor

import (
	"context"

	"my.favorite/cclawservice/harness/toolcontract"
)

type ISandboxSpecAware interface {
	SandboxSpec(ctx context.Context, input map[string]any, cc toolcontract.CallContext) *SandBoxSpec
}
