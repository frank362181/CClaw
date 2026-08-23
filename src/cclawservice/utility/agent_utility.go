/**
* Markdown 文档 YAML Front Matter 的拆分工具
*
* 约定:文档顶部的 --- 与 --- 之间为 YAML 元数据(如 name/description/summary 等),
* 其余部分为正文。本工具只负责"拆分",具体字段的解析由各强类型结构体完成。
*
* stripFrontMatter / extractFrontmatterBlock:
* 正文必须被完整保留 —— 没有 front matter 的文档,整个原文即正文。
**/

package utility

import (
	"strings"

	myException "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/model/dataobject"
)

// FrontMatterDelimiter front matter 的定界符
const FrontMatterDelimiter = "---"
const OwnerIdPlatform = "000000"

// SplitFrontMatter 把 markdown 文档拆分成 (frontMatter, content) 两部分。
// 只有当文档以 "---" 行开头并且存在闭合的 "---" 行时才认为存在 front matter;
// 否则返回空 frontMatter 与完整原文,保证普通 markdown 文档的正文不丢失。
// 兼容 CRLF(\r\n) 与 BOM 前缀。
func SplitFrontMatter(data []byte) (string, string) {
	// 统一行尾,兼容 Windows 编辑保存的文档
	text := strings.ReplaceAll(string(data), "\r\n", "\n")

	// 去掉 BOM 与行首空白后,必须仍以 --- 开头
	trimmed := strings.TrimLeft(text, "\ufeff \t\r\n")
	if !strings.HasPrefix(trimmed, FrontMatterDelimiter+"\n") {
		return "", text
	}

	rest := strings.TrimPrefix(trimmed[len(FrontMatterDelimiter):], "\n")

	// 查找闭合的 --- 行;未闭合则视为普通 markdown,正文为完整原文
	closeIdx := strings.Index(rest, "\n"+FrontMatterDelimiter)
	if closeIdx == -1 {
		return "", text
	}

	frontMatter := rest[:closeIdx]
	after := rest[closeIdx+len("\n"+FrontMatterDelimiter):]

	return strings.TrimSpace(frontMatter), strings.TrimPrefix(after, "\n")
}

// HasFrontMatter 判断文档是否携带 YAML front matter(以 --- 行开头且有闭合 --- 行)
func HasFrontMatter(data []byte) bool {
	frontMatter, _ := SplitFrontMatter(data)
	return len(frontMatter) > 0
}

// StripFrontMatter 返回剥离 front matter 之后的正文(与 OpenClaw stripFrontMatter 语义一致)
func StripFrontMatter(data []byte) string {
	_, content := SplitFrontMatter(data)
	return content
}

// ========================== 以下函数是对agent,llm provider,skills进行规整和校验 ============================

func NormalizeOwner(ownerId *string) {
	if strings.TrimSpace(*ownerId) == "" {
		*ownerId = OwnerIdPlatform // 平台默认归属,与 DDL 默认值一致
	}
}

func ValidateLLMProvider(p dataobject.TLLMProvider) error {
	if strings.TrimSpace(p.ProviderId) == "" {
		return myException.AgentParameterErrEx("provider id is empty")
	}
	if strings.TrimSpace(p.ProviderName) == "" {
		return myException.AgentParameterErrEx("provider name is empty")
	}
	if strings.TrimSpace(p.BaseUrl) == "" {
		return myException.AgentParameterErrEx("provider base url is empty")
	}
	return nil
}

func ValidateAgent(a dataobject.TAgent) error {
	if strings.TrimSpace(a.AgentId) == "" {
		return myException.AgentParameterErrEx("agent id is empty")
	}
	if strings.TrimSpace(a.AgentName) == "" {
		return myException.AgentParameterErrEx("agent name is empty")
	}
	if strings.TrimSpace(a.PrimaryModel) == "" {
		return myException.AgentParameterErrEx("agent primary model is empty")
	}
	if strings.TrimSpace(a.AgentDir) == "" {
		return myException.AgentParameterErrEx("agent dir is empty")
	}
	if strings.TrimSpace(a.Workspace) == "" {
		return myException.AgentParameterErrEx("agent workspace is empty")
	}
	return nil
}

func ValidateSkill(sk dataobject.TSkill) error {
	if strings.TrimSpace(sk.SkillId) == "" {
		return myException.AgentParameterErrEx("skill id is empty")
	}
	if strings.TrimSpace(sk.SkillName) == "" {
		return myException.AgentParameterErrEx("skill name is empty")
	}
	if strings.TrimSpace(sk.SkillDir) == "" {
		return myException.AgentParameterErrEx("skill dir is empty")
	}
	if sk.SkillScope < dataobject.SkillScopeSystem || sk.SkillScope > dataobject.SkillScopePlugin {
		return myException.AgentParameterErrEx("skill scope is invalid")
	}
	return nil
}
