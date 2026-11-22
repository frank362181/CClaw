/**
* 面向所有业务开放的接口
**/

package common

import (
	"encoding/json"

	hCommon "my.favorite/cclawservice/harness/common"
)

type Spec interface {
	Fields() map[string]string

	// WithField 返回副本并写入一个字段
	// 用途:系统生成的字段(如 archive_no)在协商之后才能确定
	// 而Spec是接口值,不能直接改字段
	WithField(key, value string) Spec

	Brief() string
}

// INegotiationSpec 协商协议的业务侧实现,三件事:参数长什么样、怎么解析、判定说明写什么
// 若有其他业务,则实现该接口即可
type INegotiationSpec interface {
	// ParamsSchema 终止工具里spec对象字段
	ParamsSchema() map[string]any

	// ParamsRequired 必填字段
	ParamsRequired() []string

	Guard() string // 技能判定说明,写入 system prompt

	DecodeParams(raw json.RawMessage) (any, error)

	// ToSpec 把协商出来的参数收敛成Spec
	ToSpec(raw any, fm hCommon.SkillFrontMatter) (Spec, bool)

	// Validate 业务约束校验;返回可读的问题,空 = 通过
	Validate(spec Spec, fm hCommon.SkillFrontMatter) []string

	// Default force模式使用缺省的spec
	Default(fm hCommon.SkillFrontMatter) (Spec, []string)

	FormatIssues(issues []string) string
	FormatIssuesForUser(issues []string) string

	// EnsureID 补齐由系统生成的业务字段
	EnsureID(spec Spec, persisted map[string]string) Spec

	// ReadPaths 返回该spec 工具将要读取的路径
	ReadPaths(spec Spec) []string
}
