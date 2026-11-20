/**
* 数据库对关键表、列、索引进行自检的函数
* 用于在系统启动时
**/

package dataaccessor

import (
	"fmt"
	"strings"

	exception2 "my.favorite/cclawservice/exception"
)

// orchestratorUpsertKey 描述各表 upsert 所依赖的唯一键。
//
// 这里只校验“列组合上是否存在唯一约束”，不校验索引名 ——
// ON CONFLICT(...) 依赖的是唯一性本身，索引叫什么名字无关
// 按名字硬编码会在索引改名/换名后产生假报错
var orchestratorUpsertKey = []struct {
	Table   string
	Columns []string
}{
	{"t_plan", []string{"turn_id"}},
	{"t_plan_task", []string{"turn_id", "task_id"}},
	{"t_plan_gap", []string{"turn_id", "round", "task_id", "code"}},
	{"t_artifact", []string{"turn_id", "name", "rev"}},
}

var orchestratorSchema = map[string][]string{
	"t_plan": {"turn_id", "session_id", "skill_id", "spec_json", "platter_json", "json_digest",
		"waves", "task_count", "est_tokens", "est_latency_ms", "lint_ok", "created_at", "updated_at"},
	"t_plan_task": {"turn_id", "task_id", "kind", "action", "section", "status", "tries",
		"started_at", "ended_at", "input_digest", "output_name", "gate_pass", "err", "updated_at"},
	"t_plan_gap": {"turn_id", "round", "code", "task_id", "section", "severity", "detail",
		"fixed", "created_at"},
	"t_artifact": {"turn_id", "name", "rev", "kind", "ref", "digest", "size",
		"fields_json", "created_at"},
	"t_intention_decision": {"turn_id", "skill_id", "decision", "reason", "missing",
		"rounds", "elapsed_ms", "created_at"},
}

func (db *OrchestratorAccessor) CheckSchema() error {
	problems := make([]string, 0, 8)

	for table, expectCols := range orchestratorSchema {
		existing := make([]string, 0, 16)
		query := fmt.Sprintf("select name from pragma_table_info(%s)", quoteSQLiteName(table))
		if err := db.gormDb.Raw(query).Scan(&existing).Error; err != nil {
			problems = append(problems, fmt.Sprintf("%s: 读取结构失败(%v)", table, err))
			continue
		}
		if len(existing) == 0 {
			problems = append(problems, table+": 表不存在")
			continue
		}

		have := make(map[string]struct{}, len(existing))
		for _, col := range existing {
			have[col] = struct{}{}
		}

		miss := make([]string, 0, 4)
		for _, col := range expectCols {
			if _, ok := have[col]; !ok {
				miss = append(miss, col)
			}
		}
		if len(miss) > 0 {
			problems = append(problems, fmt.Sprintf("%s: 缺列 %s", table, strings.Join(miss, ",")))
		}
	}

	for _, key := range orchestratorUpsertKey {
		ok, err := db.hasUniqueKey(key.Table, key.Columns)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: 唯一键检查失败(%v)", key.Table, err))
			continue
		}
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: 缺唯一约束 (%s)",
				key.Table, strings.Join(key.Columns, ",")))
		}
	}

	if len(problems) == 0 {
		return nil
	}

	return exception2.DatabaseExecErrEx2("orchestrator schema mismatch: %s", strings.Join(problems, "; "))
}

// hasUniqueKey 判断 table 上是否存在“列组合恰好等于 cols”的唯一索引（顺序无关）。
// 这是 ON CONFLICT(cols) 能成立的充要条件，比匹配索引名更贴近真实约束。
func (db *OrchestratorAccessor) hasUniqueKey(table string, cols []string) (bool, error) {
	indexes := make([]string, 0, 8)
	listQuery := fmt.Sprintf(`select name from pragma_index_list(%s) where "unique" = 1`,
		quoteSQLiteName(table))
	if err := db.gormDb.Raw(listQuery).Scan(&indexes).Error; err != nil {
		return false, err
	}

	want := make(map[string]struct{}, len(cols))
	for _, col := range cols {
		want[col] = struct{}{}
	}

	for _, index := range indexes {
		got := make([]string, 0, 8)
		infoQuery := fmt.Sprintf("select name from pragma_index_info(%s) order by seqno",
			quoteSQLiteName(index))
		if err := db.gormDb.Raw(infoQuery).Scan(&got).Error; err != nil {
			return false, err
		}
		if len(got) != len(want) {
			continue
		}

		matched := true
		for _, col := range got {
			if _, ok := want[col]; !ok {
				matched = false
				break
			}
		}
		if matched {
			return true, nil
		}
	}

	return false, nil
}

// quoteSQLiteName 把标识符包成 SQLite 单引号字符串字面量
// pragma_table_info / pragma_index_list / pragma_index_info 是表值函数，
// 参数必须是字符串字面量，不能使用绑定参数。
func quoteSQLiteName(name string) string {
	return "'" + strings.ReplaceAll(name, "'", "''") + "'"
}
