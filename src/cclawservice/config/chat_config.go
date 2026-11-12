/**
* chat锁需要的配置信息,来源于INI/JSON等配置文件
* 包含聊天、工具调用、agent是否路由等
**/

package config

import pConfig "my.favorite/platformutility/config"

type ChatConfig struct {
	Routing struct {
		Enabled              bool    // 是否开启路由
		DryRun               bool    // 照常进行判断并写库,但执行仍然按照单Agent
		AllowDelegate        bool    // 灰度期使用:false分诊照跑但一律走单agent
		DelegateConfidence   float64 // 进入delegate的置信度门槛
		StrongSignalOverride bool    // 命中结构强信号时,及时LLM时也强制进入B段
		MinCapabilitySpan    int     // 能力跨度阈值:涉及多个agent时才算复杂
		MinMaterialsStrong   int     // 材料数量强信号阈值
		KeywordRuleMode      string  //关键词规则的语义:hint(加分)/hard直接(single)
		QualityPreset        string  // 仅映射delegateConfidence/minMaterialsStrong
	}

	ToolRule struct {
		PolicyEnabled   bool // 是否开启tool调用时的策略
		FunctionCalling bool

		AllowShell               bool // 是否开启支持bash类没有路径参数的命令
		SandboxEnabled           bool // 是否开启沙箱环境
		AutoAllowBashIfSandboxed bool

		// 读写工具允许的根,以;分隔
		ReadRoots  []string
		WriteRoots []string

		// 绝对禁止的路径
		DenyRoots []string
	}

	Chat struct {
		/**
		# sessionRecord: 会话记录开关(阶段1双写)。
		#   true  = 把会话/轮次/工具调用写入 t_session/t_turn/t_turn_tool,
		#           并把内容追加到 <exeDir>/cclaw/chat_history/<sessionId>/memory_{datetime}.md;
		#   false = 完全不记录(纯旁路,不影响聊天主流程)。
		# 说明:该记录目前“只写不读”,不参与 system prompt 与上下文装配;
		#       任一环节失败仅记日志,不会影响给用户的回复
		**/
		SessionRecorded bool
	}
}

func LoadChatConfig(helper *pConfig.ConfigurationHelper) (ChatConfig, error) {
	var c ChatConfig
	if helper == nil {
		return c, nil
	}

	// routing configuration
	c.Routing.Enabled, _ = helper.GetBoolValueDefault("routing", "enabled", false)
	c.Routing.DryRun, _ = helper.GetBoolValueDefault("routing", "dryRun", true)
	c.Routing.AllowDelegate, _ = helper.GetBoolValueDefault("routing", "allowDelegate", false)
	c.Routing.DelegateConfidence, _ = helper.GetFloat64ValueDefault("routing", "delegateConfidence", 0.6)
	c.Routing.StrongSignalOverride, _ = helper.GetBoolValueDefault("routing", "strongSignalOverride", true)
	c.Routing.MinCapabilitySpan, _ = helper.GetIntValueDefault("routing", "minCapabilitySpan", 2)
	c.Routing.MinMaterialsStrong, _ = helper.GetIntValueDefault("routing", "minMaterialsStrong", 3)
	c.Routing.KeywordRuleMode, _ = helper.GetStringValueDefault("routing", "keywordRuleMode", "hint")
	c.Routing.QualityPreset, _ = helper.GetStringValueDefault("routing", "qualityPreset", "balanced")

	return c, nil
}
