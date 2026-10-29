/**
* 一次工具调用的 **结果值对象**
**/

package toolexecutor

import "fmt"

// ToolOutcome 一次工具调用的结果值对象。
// [关键不变量] 把两条互相冲突的消息口径封装在类型里,调用点不需要自己决定怎么截断:
//   - ForRecord: 写历史/旁路记录 --- 完整原文(DB 的 256 rune 截断由 journal 负责)
//   - ForModel : 回填给模型 --- 截断到 maxRunes(总长含省略号)
//
// ForModel 是"回填策略",属于编排层的投影,不在本包实现(见 chat.ForModel)。
type ToolOutcome struct {
	Seq    int
	CallID string
	Name   string
	Arg    string

	Full   string // 工具调用的完整原始输出
	Reason error  // 非nil表示执行失败

	CallAt, RespAt int64
	ElapseMs       int64 // 工具执行耗费的时间,毫秒
}

func (o ToolOutcome) Failed() bool { return o.Reason != nil }

// ForRecord 返回写入历史/旁路记录的文本:完整/截断
func (o ToolOutcome) ForRecord() string {
	if o.Reason != nil {
		return fmt.Sprintf(`{"error":%q}`, o.Reason.Error())
	}

	return o.Full
}
