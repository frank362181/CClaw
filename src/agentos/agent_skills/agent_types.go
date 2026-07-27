/*
* 定义AI助手所需的各种类型
 */

package agent_skills

// SkillRequires SKILL所需的配置和环境
type SkillRequires struct {
	bins    []string
	anyBins []string
	env     []string
	config  []string
}

// SkillMetadata skill的元数据配置
type SkillMetadata struct {
	skillKey   string
	primaryEnv string
	emoji      string

	homePage string
	os       []string

	requires SkillRequires
}

// SkillInvocationPolicy SKILL调用策略
type SkillInvocationPolicy struct {
	userInvocable         bool
	disableModeInvocation bool
}

type FrontMatter struct {
}
