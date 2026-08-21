/**
* SKILL.md 技能文档的数据对象
*
* SKILL.md 的 YAML Front Matter(元数据)字段与 agentos/agent_skills 的 SkillMetadata 保持一致:
* name / description / user-invocable / disable-mode-invocation / emoji / home_page / os / requires
**/

package dataobject

import "encoding/json"

// SkillRequires 是 SKILL 运行所需的依赖环境
type SkillRequires struct {
	Bins    []string `json:"bins,omitempty" yaml:"bins,omitempty"`
	AnyBins []string `json:"anyBins,omitempty" yaml:"any_bins,omitempty"`
	Env     []string `json:"env,omitempty" yaml:"env,omitempty"`
	Config  []string `json:"config,omitempty" yaml:"config,omitempty"`
}

// SkillFrontMatter 是 SKILL.md 的 YAML 元数据部分
type SkillFrontMatter struct {
	Name                  string        `json:"name,omitempty" yaml:"name,omitempty"`
	Description           string        `json:"description,omitempty" yaml:"description,omitempty"`
	UserInvocable         bool          `json:"userInvocable,omitempty" yaml:"user-invocable,omitempty"`
	DisableModeInvocation bool          `json:"disableModeInvocation,omitempty" yaml:"disable-mode-invocation,omitempty"`
	Emoji                 string        `json:"emoji,omitempty" yaml:"emoji,omitempty"`
	HomePage              string        `json:"homePage,omitempty" yaml:"home_page,omitempty"`
	OS                    []string      `json:"os,omitempty" yaml:"os,omitempty"`
	Version               string        `json:"version,omitempty" yaml:"version,omitempty"`
	Requires              SkillRequires `json:"requires,omitempty" yaml:"requires,omitempty"`
}

// SkillDocument 表示一个已解析的 SKILL.md 文档
type SkillDocument struct {
	SkillId     string           `json:"skillId"`
	SkillFile   string           `json:"skillFile"` // SKILL.md 的完整路径
	FrontMatter SkillFrontMatter `json:"frontMatter"`
	Content     string           `json:"content"`
	ByteSize    int64            `json:"byteSize"`
}

// ToJsonString 序列化为 JSON 字符串
func (doc *SkillDocument) ToJsonString() (string, error) {
	jsonStr, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}

	return string(jsonStr), nil
}

// FromJsonString 从 JSON 字符串反序列化
func (doc *SkillDocument) FromJsonString(jsonStr string) error {
	return json.Unmarshal([]byte(jsonStr), doc)
}
