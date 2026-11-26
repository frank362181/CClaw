/**
* 面向所有业务开放的接口
* 仅对需要应用plan mode生效,单轮对话或简单工具调用不需要
**/

package common

import (
	"encoding/json"

	hCommon "my.favorite/cclawservice/harness/common"
)

// ISpec 所有的业务SKILL解析参数时需要实现的接口
type ISpec interface {
	Fields() map[string]string

	// WithField 返回副本并写入一个字段
	// 用途:系统生成的字段(如 archive_no)在协商之后才能确定
	// 而Spec是接口值,不能直接改字段
	WithField(key, value string) ISpec

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
	ToSpec(raw any, fm hCommon.SkillFrontMatter) (ISpec, bool)

	// Validate 业务约束校验;返回可读的问题,空 = 通过
	Validate(spec ISpec, fm hCommon.SkillFrontMatter) []string

	// Default force模式使用缺省的spec
	Default(fm hCommon.SkillFrontMatter) (ISpec, []string)

	FormatIssues(issues []string) string
	FormatIssuesForUser(issues []string) string

	// EnsureID 补齐由系统生成的业务字段
	EnsureID(spec ISpec, persisted map[string]string) ISpec

	// ReadPaths 返回该spec 工具将要读取的路径
	ReadPaths(spec ISpec) []string
}
