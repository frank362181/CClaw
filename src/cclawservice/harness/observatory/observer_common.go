/*
* 定义observer需要的所有数据类型,但常量定义在observer_const.go文件中
 */

package observatory

// SkillSkipReason 一个被跳过的技能及其原因
type SkillSkipReason struct {
	SkillFile string
	Reason    string
}

// SkillLoadReport 一次 LoadSkills 的观测结果(命中/重载/跳过 + 原因)
type SkillLoadReport struct {
	Requested  int // 入参技能数
	Loaded     int // 返回的可用技能数
	Cached     int // 命中缓存(零读盘零解析)
	Reloaded   int // 首次加载或指纹变化:读盘 + YAML 解析
	Duplicated int // 重复绑定被去掉的条数

	// Skipped 被跳过的技能:分两批记录 —— 先记入参非法(目录为空/越界),
	// 再记读盘阶段失败(文件缺失/过大/YAML 处理失败)。每条带 SkillFile 可唯一定位。
	Skipped []SkillSkipReason
}

// TokenUsageEvent 一次上游调用的token 用量
type TokenUsageEvent struct {
	ProviderId string
	Model      string
	Stream     bool

	Prompt int // prompt tokens(含 system + 历史 + 工具结果)
	Cached int // 命中前缀缓存的输入 token(已归两口径)

	HasCacheReport bool // 上游是否上报了缓存字段(区分:0、命中、不支持)

	InputCost      float32 // 成本,来自t_llm
	CachedReadCost float32
}

// PromptCacheSummary 一行汇总
type PromptCacheSummary struct {
	Calls  int
	Prompt int
	Cached int

	HitRatePercent float64 // 只是统计上报了缓存字段的调用
	SavedMoney     float64

	NoReport int //未上报缓存字段的调用数(其命中率不可知,不应计入分母)
}
