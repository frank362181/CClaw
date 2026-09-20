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
	BasicTool

	Tools           []BasicTool //skill.md中定义的front matter中的工具
	AllowedTools    []string
	DisallowedTools []string

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
