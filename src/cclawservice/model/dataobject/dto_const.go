package dataobject

// 注册用户类型
const (
	AccountCatPlatform   = iota //平台用户
	AccountCatPerson            // 个人用户
	AccountCatEnterprise        //企业用户
)

// SKILL的作用域
const (
	SkillScopeSystem = iota
	SkillScopePersonal
	SkillScopeProject
	SkillScopePlugin
)
