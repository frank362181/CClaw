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
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"my.favorite/cclawservice/common"
	"my.favorite/cclawservice/controller/model"
	myException "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
	pException "my.favorite/platformutility/exception"
	"my.favorite/platformutility/webbase/middleware/basic"
)

func GetAuthentication(c *gin.Context) (*model.LoginSession, bool) {
	tempValue, exists := c.Get(common.ContextAuthenticatedUser)
	if !exists {
		basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusUnauthorized, common.ConstErrorUserUnAuthorized, common.ConstErrorUserUnAuthorizedMsg, nil)
		return nil, false
	}
	loginSession, ok := tempValue.(*model.LoginSession)
	if !ok {
		basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusInternalServerError, basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
		return nil, false
	}

	return loginSession, true
}

// FrontMatterDelimiter front matter 的定界符
const FrontMatterDelimiter = "---"

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
		*ownerId = common.OwnerIdPlatform // 平台默认归属,与 DDL 默认值一致
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
	if !pCommon.IsSafeDir(a.AgentDir) {
		return myException.AgentParameterErrEx("agent dir is an illegal path")
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

// BuildAgentWorkspaceRoot 根据agent的信息构建agent的工作目录
// 安全:agent_dir 必须为合法相对路径,防止工作区根逃出 <exeDir>/workspace。
func BuildAgentWorkspaceRoot(agent dataobject.TAgent) (string, bool) {
	if !pCommon.IsSafeDir(agent.AgentDir) {
		return "", false
	}

	agentWorkspaceRoot := filepath.Join(pCommon.GetExecutableDir(), common.AgentServiceWorkspaceDirName, agent.OwnerId, agent.AgentDir)
	if pCommon.IsDir(agentWorkspaceRoot) {
		return agentWorkspaceRoot, true
	}

	return "", false
}

// ResolveStartupPath 校验并解析启动文档路径:仅允许纯文件名,禁止目录穿越与符号链接逃逸。
// 供 prompt 组装(LoadWorkspace)与启动文件管理(StartupFileRepository)共用,
// 保证同一套安全路径规则。文件名非法/越界时返回平台 FilePathError。
func ResolveStartupPath(workspaceRoot, fileName string) (string, error) {
	// 只允许纯文件名:不能包含路径分隔符、"." 或 ".."
	if filepath.Base(fileName) != fileName || fileName == "." || fileName == ".." {
		return "", pException.FilePathErrorEx("illegal document file name: " + fileName)
	}

	return pCommon.SafeJoin(workspaceRoot, filepath.Join(workspaceRoot, fileName))
}

// FindStartupDocFile 按文件名(精确匹配)在启动文件清单中查找,未命中返回 ok=false。
func FindStartupDocFile(fileName string) (common.StartupDocSpec, bool) {
	for _, f := range common.StartupDocFiles {
		if f.Name == fileName {
			return f, true
		}
	}
	return common.StartupDocSpec{}, false
}
