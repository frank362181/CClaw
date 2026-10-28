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

// LlmCallTiming 一次上游LLM调用的时间(与TokenUsageEvent同源,同一次调用)
// 只在 LLM客户端内部可得: TTFT 依赖 SSE 读循环,上层无法伪造
type LlmCallTiming struct {
	TurnScope string // 合并归因标识(turnId);若为空则未归因

	ProviderId string
	Model      string
	Stream     bool

	// 本次请求 "发出时刻" 的毫秒数
	StartOffsetMs int64

	TtftMs  int64 // 请求发出 -> 上游首个data行,非首个字节
	TotalMs int64 // 请求发出 -> 本轮结束

	PromptTokens int
	CachedTokens int

	Err string // 非空 = 本次调用失败(慢失败同样要留痕,否则看不到"卡住的那次")
}

type TurnTiming struct {
	TurnScope string
	AgentId   string

	GateMs   int64 // ①.Agent并发闸等待
	RouteMs  int64 // ②.路由:会话绑定/关键词/LLM分类
	SkillMs  int64 // ③.技能读盘 + 工具合成
	PromptMs int64 // ④.意图们 + system prompt 组装 + 首轮计划/预算
	LlmMs    int64 // ⑤.所有上游调用 TotalMs 之和
	ToolsMs  int64 // ⑥.所有工具执行之和

	Rounds    int
	ToolCalls int

	FirstVisibleMs int64 //回合开始 -> 首个"内容类"事件(正文/思考/工具);不包含status

	ReplyRunes int
	TotalMs    int64
	Err        string
}
