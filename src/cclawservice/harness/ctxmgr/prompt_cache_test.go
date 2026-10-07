/**
* 稳定前缀本地缓存的测试。
*
* 缓存成立的两个前提,必须同时锁住:
*  1) **同内容 ⇒ 同键**(入参顺序抖动不该换键,否则缓存形同虚设);
*  2) **内容变 ⇒ 换键**(否则会拿到过期前缀:改了技能/工具/MEMORY 却仍命中旧字节)。
**/

package ctxmgr

import (
	"testing"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/model/dataobject"
)

// TestPromptKeyStability 锁定"缓存键只由内容指纹决定"。
func TestPromptKeyStability(t *testing.T) {
	agent := dataobject.TAgent{AgentId: "test_agent_prompt_key"}
	skillsA := []hCommon.SkillDocument{
		{SkillFile: "/s/a/SKILL.md", Fingerprint: "fa"},
		{SkillFile: "/s/b/SKILL.md", Fingerprint: "fb"},
	}
	skillsReordered := []hCommon.SkillDocument{
		{SkillFile: "/s/b/SKILL.md", Fingerprint: "fb"},
		{SkillFile: "/s/a/SKILL.md", Fingerprint: "fa"},
	}
	skillsChanged := []hCommon.SkillDocument{
		{SkillFile: "/s/a/SKILL.md", Fingerprint: "fa2"},
		{SkillFile: "/s/b/SKILL.md", Fingerprint: "fb"},
	}
	tools := []hCommon.ToolPromptItem{{Name: "read", Description: "读取本地文件", Source: hCommon.TSourceSystem}}
	toolsChanged := []hCommon.ToolPromptItem{{Name: "write", Description: "写入本地文件", Source: hCommon.TSourceSystem}}

	base := promptKey(agent, hCommon.BootstrapScopeNormal, skillsA, tools)

	if base != promptKey(agent, hCommon.BootstrapScopeNormal, skillsReordered, tools) {
		t.Fatalf("技能入参顺序不应换键(会导致缓存穿透)")
	}
	if base == promptKey(agent, hCommon.BootstrapScopeNormal, skillsChanged, tools) {
		t.Fatalf("技能指纹变化必须换键(否则技能更新后仍命中旧前缀)")
	}
	if base == promptKey(agent, hCommon.BootstrapScopeNormal, skillsA, toolsChanged) {
		t.Fatalf("工具清单变化必须换键")
	}
	if base == promptKey(agent, hCommon.BootstrapScopeCron, skillsA, tools) {
		t.Fatalf("scope 变化必须换键")
	}
	if base == promptKey(dataobject.TAgent{AgentId: "test_agent_prompt_key_other"}, hCommon.BootstrapScopeNormal, skillsA, tools) {
		t.Fatalf("agent 变化必须换键(否则会串会话)")
	}
}

// TestGetSystemPromptCacheHit 验证稳定前缀的跨轮复用:相同输入第二次必须命中同一对象。
func TestGetSystemPromptCacheHit(t *testing.T) {
	agent := dataobject.TAgent{AgentId: "test_agent_prompt_cache_hit"}

	first, err := getSystemPrompt(agent, hCommon.BootstrapScopeNormal, nil, nil)
	if err != nil {
		t.Fatalf("首次组装失败: %s", err.Error())
	}
	second, err := getSystemPrompt(agent, hCommon.BootstrapScopeNormal, nil, nil)
	if err != nil {
		t.Fatalf("二次组装失败: %s", err.Error())
	}
	if first != second {
		t.Fatalf("相同输入应命中缓存并返回同一对象")
	}
	if first.Prompt != second.Prompt {
		t.Fatalf("缓存命中时字节必须完全一致")
	}
}
