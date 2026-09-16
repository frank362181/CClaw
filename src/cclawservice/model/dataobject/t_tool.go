/**
* 工具数据对象
*
* 严格对齐 data/ddl_sqlite3.sql 中与工具相关的表:
*   - TTool   → t_tool   工具表
*
* 字段命名沿用既有约定:显式 gorm column 标签 + 与列名一致的 Json 标签。
**/

package dataobject

type TTool struct {
	ToolId      string `gorm:"column:tool_id" json:"ToolId"`
	ToolName    string `gorm:"column:tool_name" json:"ToolName"`
	Description string `gorm:"column:description" json:"Description"`
	Script      string `gorm:"column:script" json:"Script"`
	InputSchema string `gorm:"column:input_schema" json:"InputSchema"`
	Disabled    bool   `gorm:"column:disabled" json:"Disabled"`
	Deleted     bool   `gorm:"column:deleted" json:"Deleted"`
}

func (t TTool) TableName() string {
	return "t_tool"
}
