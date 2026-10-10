/*
* MEMORY.md 分为 "人工维护区/自动记忆区"切分(装配侧/写入侧的唯一口径)
*
* 文件形态:
*	MEMORY.md  -- 长期记忆
*	... 人工维护内容(模型用不改写)...
*	<!--auto-memory:updated=...;watermarks=... -> 锚点
*	-事实
*	<!--/auto-memory -->
* 这么设计的原因:
*	装配侧(ctxmgr)需要"人工区进稳定前缀、自动区后置注入",
*	写入侧(knowledge)需要按同一套标记定位自动区 -- 两处必须用同一套标记与同一套边界规则
 */

package ctxmgr

import (
	"fmt"
	"regexp"
	"strings"

	hCommon "my.favorite/cclawservice/harness/common"
	pCommon "my.favorite/platformutility/common"
)

const (
	MemoryAutoBeginPrefix = "<!-- auto-memory:"     //自动区起始前缀
	MemoryAutoEndMarker   = "<!-- /auto-memory -->" //自动区结束标记
)

// 压缩部分 ==============================

const (
	CompactionUserHeader = "请压缩下面这段历史(记录序号 %d-%d):\n\n"

	// CompactionMergeUserHeader 合并摘要时的用户提示:输入是"已有摘要正文"而非原始记录。
	CompactionMergeUserHeader = "请把下面这组历史摘要合并为一条更精炼的摘要(记录序号 %d-%d)。\n" +
		"它们将被你的输出取代,所以必须覆盖它们**全部**的信息,不得丢失要点:\n\n"
)

// CompactionSystemPrompt 压缩提示词
// 长度上限必须**显式写给模型**:模型自己理解"简明"时,输出长度会随输入一起膨胀,
// 而 MaxSummaryRunes(落盘截断)与上游 max_tokens 都是硬上限 —— 一旦顶到,摘要会被截断/报错,
// 整轮压缩白跑(且下一轮还会挑中同一段,形成"永远压不掉"的死循环)。
var CompactionSystemPrompt = fmt.Sprintf("你是对话历史压缩器。你的任务是把给定的历史片段压缩为简明摘要,供后续对话继续使用。\n"+
	"要求:\n"+
	"1. 保留事实性信息:用户的目标与偏好、已确认的结论、关键数据(数字、编号、日期)、"+
	"文件路径与命令、尚未完成的待办事项及其状态;\n"+
	"2. 删除寒暄、重复表述与无关细节;\n"+
	"3. 只依据给定内容,不得推测或补造任何信息;不确定的内容标注为“未确认”;\n"+
	"4. 用第三人称陈述,按时间顺序分条列出,不要输出标题之外的多余格式;\n"+
	"5. 若片段中有工具调用结果,只保留结论性的数据,不要复述原始输出全文;\n"+
	"6. 摘要长度不超过 %d 个字符(中文字符按 1 个计),宁可更短。", hCommon.MaxSummaryRunes)

// CompactionMergeSystemPrompt 合并摘要专用系统提示(输入是摘要而非原始记录)。
// 规则编号接在 compactionSystemPrompt 之后(它已经用到 6)
var CompactionMergeSystemPrompt = CompactionSystemPrompt + "\n" +
	"7. 这次输入本身已经是摘要:你要做的是合并同类项,保留全部事实与结论,只删重复表述。"

// KeyTokenPattern 关键信息采样:文件路径、长数字(编号/金额/日期)、带扩展名的文件名
var KeyTokenPattern = regexp.MustCompile(`(?:[A-Za-z]:)?(?:[\w.\-]+/)+[\w.\-]+|\b\d{4,}\b|\b[\w\-]{2,}\.(?:go|json|md|ini|sql|txt|log|yaml|yml|sh|bat)\b`)

// 蒸馏部分 ==============================

// 蒸馏提示词
// 作用域约束是本提示词最重要的部分:MEMORY.md 是 (租户, agent) 级共享文件,
// 会被注入该 agent 的每一次请求。写错范围等于把用户个人信息变成所有人的 system prompt。
const (
	DistillSystemPrompt = "你负责从对话中提炼**长期有效**的记忆,写入智能体的长期记忆文件。\n" +
		"这个文件由同一单位下的**所有人共用**,并会出现在后续每一次对话的开头,所以:\n" +
		"\n" +
		"【只提炼单位/智能体级、跨会话仍然成立的信息】例如:\n" +
		"- 项目与系统的稳定事实(目录结构、服务地址、命名与编码规范、字段口径);\n" +
		"- 业务规则与已确认的结论(为什么这么做、被否决的方案及原因);\n" +
		"- 本单位的固定约定(交付物格式、存放位置、术语用法);\n" +
		"- 尚未完成的长期待办。\n" +
		"\n" +
		"【绝对不要提炼】\n" +
		"- 个人隐私与用户个人信息:姓名、称呼、联系方式、账号、个人喜好(喜欢的花/城市/数字)、" +
		"个人行程与历史查询记录等 —— 这些不属于共享记忆,即使对话里反复出现也不要写;\n" +
		"- 一次性的内容:本次问答过程、临时计算结果、即时查询结果(天气/汇率/股价等)、寒暄。\n" +
		"\n" +
		"输出要求:\n" +
		"1. 每条一行,以 \"- \" 开头,一句话说清,不写理由过程;\n" +
		"2. 不得推测或补造,只依据给定对话;\n" +
		"3. 没有任何值得长期记住的内容时,只输出 " + hCommon.DistillNoneMark + "。"

	DistillUserHeader = "以下是一段对话记录(记录序号 %d-%d),请提炼长期记忆:\n\n"

	// DistillExistingHeader 已有长期记忆的表头。
	DistillExistingHeader = "【已有长期记忆】(上次蒸馏得到的当前状态)\n"

	// DistillRewriteNotice 收尾指令:要求输出"更新后的完整列表",并**显式给出长度上限**
	// (模型自己猜"简明"时容易写到上限之外,而超长输出会被 max_tokens 截断 → 整轮作废)。
	DistillRewriteNotice = "请输出**更新后的完整长期记忆列表**(每行一条,以 \"- \" 开头):" +
		"保留仍然有效的条目;把被取代的改写为新说法;删掉已失效、已完成或一次性的条目;" +
		"合并重复表述;总计不超过 %d 条,每条不超过 %d 个字符;" +
		"没有任何需要记住的内容时只输出 %s。\n"

	// DistillAppendNotice 追加模式的收尾指令:只要新增,不要重述已有列表
	DistillAppendNotice = "请只输出**本次对话中新增的**长期事实(每行一条,以 \"- \" 开头);" +
		"已有记忆无需重复输出;没有任何新增时只输出 %s。\n"
)

var (
	// MemoryAutoAnchorPattern 匹配自动区锚点注释:兼容"仅标记"与"带属性(update /watermarks)"两种写法
	// 注意:它只用于 **定位边界**,属性解析由写入侧更严格的模式负责
	MemoryAutoAnchorPattern = regexp.MustCompile(`^<!--\s*auto-memory\b.*-->$`)

	// MemorySectionHeaderPattern 匹配迁移期的旧格式小节标题
	// "## 2026-09-13 01:02:03 | session xxx"(可带"|records a-b")
	MemorySectionHeaderPattern = regexp.MustCompile(`(?m)^## \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}.*$`)

	// DistillRecordsRangePattern 匹配小节标题内的段落
	DistillRecordsRangePattern = regexp.MustCompile(`\| records (\d+)-(\d+)\s*$`)

	// DistillAutoBeginPattern 自动记忆区用一个 HTML 注释定界,便于人工审阅时一眼看出"这块由系统维护":
	//
	//	<!-- auto-memory:updated=2026-09-15 10:23:45;watermarks=sess-a:120,sess-b:40 -->
	//	- 事实…
	//	<!-- /auto-memory -->
	DistillAutoBeginPattern = regexp.MustCompile(`^<!--\s*auto-memory:updated=([^;]*);watermarks=(.*?)\s*-->$`)
)

// SplitMemoryDoc 将MEMORY.md拆分成 人工维护区，自动记忆区
func SplitMemoryDoc(content string) (human, auto string) {
	if strings.TrimSpace(content) == pCommon.BlankString {
		return content, ""
	}

	lines := pCommon.SplitStringEx(content, "\n")
	start, end, ok := AutoRegionBounds(lines)
	if !ok {
		return content, ""
	}

	return strings.Join(lines[:start], "\n"), strings.Join(lines[start:end], "\n")
}

// AutoRegionBounds 返回"自动内容"在文件里占据的连续区域[start,end)
// 定位方式:先找锚点,再从锚点向后吃自动行;遇到其他内容即停
// 两项保证(与写入段历史行为一致)
//   - 前导空行不属于区域(区域从锚点行开始),反复重写不会每次吃掉一个空行
//   - 向后只吃自动行,人工维护区(手写标题/正文)永远不会删除
func AutoRegionBounds(lines []string) (int, int, bool) {
	anchor := -1
	for i := range lines {
		if isAutoAnchor(lines[i]) {
			anchor = i
			break
		}
	}

	if anchor < 0 {
		return -1, -1, false
	}

	start := anchor
	end := anchor + 1
	for end < len(lines) && isAutoRegionLine(lines[end]) {
		end++
	}

	return start, end, true
}

func isAutoAnchor(line string) bool {
	trimmed := strings.TrimSpace(line)
	return MemoryAutoAnchorPattern.MatchString(trimmed) || MemorySectionHeaderPattern.MatchString(trimmed)
}

func isAutoRegionLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == pCommon.BlankString {
		return true
	}

	if MemorySectionHeaderPattern.MatchString(trimmed) {
		return true
	}

	if MemoryAutoAnchorPattern.MatchString(trimmed) || trimmed == MemoryAutoEndMarker {
		return true
	}

	if strings.HasPrefix(trimmed, "<!--") || strings.HasSuffix(trimmed, "-->") {
		return true
	}

	//条目行:"-" + 非空文(与写入侧parseFactLine同一口径)
	return strings.HasPrefix(trimmed, "- ") && strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")) != ""
}
