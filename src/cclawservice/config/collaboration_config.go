/**
* 项目本地配置信息
*
* 唯一总开关:Routing.Enabled;Evaluate不设置Enabled是因为避免
*	"没协作却在跑评估"这种中间状态
*
* 灰度测试打开: enabled=true -> dryRun=false -> allowDelegate=true -> evaluate.shadowOnly=false
**/

package config

import pConfig "my.favorite/platformutility/config"

type CollaborationConfig struct {
	Collaboration struct {
		MaxTasks     int   // 规划的最大的步骤数量
		MaxParallels int   // 最大的并行的worker数量
		TaskTimeout  int64 // 每个任务超时时间
		StallWarn    int64 // 监控阈值:无事件多久打印一个wan
		StallKill    int64 // 监控阈值:无事件多久消除

		SummaryMaxRunes int64 // worker产出的summary最大runes长度
		TriageMaxTokens int64 // 分诊一次调用的max tokens

		LintRetries      int    // 计划linter不通过时回填的最大次数
		EvaluatorAgentId string // 专职评估的agentId
	}

	Evaluate struct {
		ShadowOnly      bool
		MinIndependence string
		InputMaxRunes   int64
		MaxRepairRounds int
		MaxRepairTasks  int
		BudgetRatio     float64
		BlockOnBlocker  bool // 表示blocker未解决时:true=降级标注后仍输出;false=整轮失败
	}
}

func LoadCollaborationConfig(helper *pConfig.ConfigurationHelper) (CollaborationConfig, error) {
	var c CollaborationConfig
	if helper == nil {
		return c, nil
	}

	// = collaboration configuration
	c.Collaboration.MaxTasks, _ = helper.GetIntValueDefault("collaboration", "maxTasks", 5)
	c.Collaboration.MaxParallels, _ = helper.GetIntValueDefault("collaboration", "maxParallels", 3)
	c.Collaboration.TaskTimeout, _ = helper.GetInt64ValueDefault("collaboration", "taskTimeout", 180)
	c.Collaboration.StallWarn, _ = helper.GetInt64ValueDefault("collaboration", "stallWarn", 60)
	c.Collaboration.StallKill, _ = helper.GetInt64ValueDefault("collaboration", "stallKill", 180)
	c.Collaboration.SummaryMaxRunes, _ = helper.GetInt64ValueDefault("collaboration", "summaryMaxRunes", 800)
	c.Collaboration.TriageMaxTokens, _ = helper.GetInt64ValueDefault("collaboration", "triageMaxTokens", 1024)
	c.Collaboration.LintRetries, _ = helper.GetIntValueDefault("collaboration", "lintRetries", 2)
	c.Collaboration.EvaluatorAgentId, _ = helper.GetStringValueDefault("collaboration", "evaluatorAgentId", "")

	// evaluate
	c.Evaluate.ShadowOnly, _ = helper.GetBoolValueDefault("evaluate", "shadowOnly", true)
	c.Evaluate.MinIndependence, _ = helper.GetStringValueDefault("evaluate", "minIndependence", "moderate")
	c.Evaluate.InputMaxRunes, _ = helper.GetInt64ValueDefault("evaluate", "inputMaxRunes", 12000)
	c.Evaluate.MaxRepairRounds, _ = helper.GetIntValueDefault("evaluate", "maxRepairRounds", 1)
	c.Evaluate.MaxRepairTasks, _ = helper.GetIntValueDefault("evaluate", "maxRepairTasks", 2)
	c.Evaluate.BudgetRatio, _ = helper.GetFloat64ValueDefault("evaluate", "budgetRatio", 0.3)
	c.Evaluate.BlockOnBlocker, _ = helper.GetBoolValueDefault("evaluate", "blockOnBlocker", true)
	return c, nil
}
