package common

type SystemicTool struct {
	BasicTool

	Tool              BasicTool
	SchemaFileName    string //对应的schema input所对应的文件
	SchemaFingerprint string //对应的schema文件的fingerprint
}

func NewSystemicTool() *SystemicTool {
	return &SystemicTool{
		BasicTool: BasicTool{
			Source: TSourceSystem,
		},
	}
}

func (st *SystemicTool) GetSchemaFileName() string {
	return st.SchemaFileName
}

func (st *SystemicTool) GetSchemaFingerprint() string {
	return st.SchemaFingerprint
}

type SkillTool struct {
	BasicTool // Name/Description 承载技能自身的名字与描述(来自 front matter)

	Tools           []BasicTool //skill.md中定义的front matter中的工具
	AllowedTools    []string
	DisallowedTools []string

	// 技能级元数据(意图门 + SOP 注入用):只记“本技能自己的”东西,
	// 不做“工具→技能”的反向归属(那个只在按技能裁剪工具时才需要)。
	TriggerKeywords []string // front matter trigger-keywords:命中才允许强制/注入正文
	ForcedTools     []string // 本技能内声明 force-call 的工具名(门按技能生效)

	SkillFileName    string
	SkillFingerprint string
}

func NewSkillTool(skillFileName, skillFingerprint string, tools []BasicTool) *SkillTool {
	return &SkillTool{
		BasicTool: BasicTool{
			Source: TSourceSkill,
		},
		SkillFileName:    skillFileName,
		SkillFingerprint: skillFingerprint,
		Tools:            tools,
	}
}

func (st *SkillTool) GetSchemaFileName() string {
	return st.SkillFileName
}

func (st *SkillTool) GetSchemaFingerprint() string {
	return st.SkillFingerprint
}
