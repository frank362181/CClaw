/*
* 系统默认的工具调用过程中的执行前,中,后等流程中的处理函数
* 包括执行前的参数修改、权限修改等
*
* 设计原则:
*	1. PreUserHookHandler 如果返回Allow不能跳过后续的deny,因此在该类方法中,Decision::Allowed返回时continue
*	   因为静态的如Deny,Ask等安全策略优先级更高
*	2.SafetyGuardrailHandler 位于 PermissionModeHandler之前,意味着即使PermissionByPass模式下也要触发
*	  SafetyGuardrailHandler的安全判断
*	3. 尾链为fail-closed:当所有的Handler都返回Next且已经到尾部时,BaseHandler.propatate()返回Deny,非Allow
*	   直接体现fail-closed原则,因为任何阶段失败都deny。权限系统中deny by default更加安全
*
**/

package security

import (
	"context"
	"fmt"
	"strings"
)

// ========================== 所有处理器父类的函数================================

func (b *BaseHandler) SetNext(next IPermissionHandler) IPermissionHandler {
	b.next = next
	return next
}

func (b *BaseHandler) Next() IPermissionHandler {
	return b.next
}

// 将决策沿着链路传递:Next 则调用下一个 Handle
// 否则短路返回
func (b *BaseHandler) propagate(ctx context.Context, req *ToolCallRequest, tool IBaseTool, d *Decision, err error) (*Decision, error) {
	if err != nil {
		return nil, err
	}

	if d.Behavior != Next {
		return d, nil
	}

	if b.next == nil {
		// 链尾仍未决策 -> fail-closed,拒绝
		return &Decision{Behavior: Deny, Reason: "permission pipeline reached end without decision (fail-closed)"}, nil
	}

	return b.next.Handle(ctx, req, tool)
}

// ========================== end of functions definition of father class =======================

// ValidateInputHandler 输入校验,step 1
type ValidateInputHandler struct {
	BaseHandler
}

func (h *ValidateInputHandler) Handle(ctx context.Context, req *ToolCallRequest, tool IBaseTool) (*Decision, error) {
	if err := tool.ValidateInput(ctx, req.Input, req.Context); err != nil {
		return &Decision{
			Behavior: Deny,
			Reason:   fmt.Sprintf("input validation failed: %s", err.Error()),
		}, nil
	}

	return h.propagate(ctx, req, tool, &Decision{Behavior: Next}, nil)
}

// PreToolUseHook 工具函数调用前被调用,step 2
type PreToolUseHook struct {
	BaseHandler
}

func (h *PreToolUseHook) Handle(ctx context.Context, req *ToolCallRequest, tool IBaseTool) (*Decision, error) {
	for _, hook := range req.Context.Hooks {
		decision, err := hook.Run(ctx, req)
		if err != nil {
			return &Decision{
				Behavior: Deny,
				Reason:   fmt.Sprintf("hook execution failed:%s", err.Error()),
			}, nil
		}

		switch decision.Behavior {
		case Deny:
			return decision, nil
		case Allow:
			// Hook返回allow不跳过后续的deny/ask规则检查
			continue
		case Modify:
			// updatedInput已由Hook写入req.Input
			// 继续到下一个Handler,后续所有检查使用修改后的输入
			continue
		default:
			continue
		}
	}

	return h.propagate(ctx, req, tool, &Decision{Behavior: Next}, nil)
}

// DenyRuleHandler 静态deny handler拒绝规则处理器,step 3
type DenyRuleHandler struct {
	BaseHandler
}

func (h *DenyRuleHandler) Handle(ctx context.Context, req *ToolCallRequest, tool IBaseTool) (*Decision, error) {
	for _, denied := range req.Context.DeniedTools {
		if MatchesTool(denied, req.ToolName) {
			return &Decision{
				Behavior: Deny,
				Reason:   fmt.Sprintf("tool %s is denied by rule %s", req.ToolName, denied),
			}, nil
		}
	}

	return h.propagate(ctx, req, tool, &Decision{Behavior: Next}, nil)
}

// AskRuleHandler 静态ask rule handler 询问规则处理器,step 4
type AskRuleHandler struct {
	BaseHandler
}

func (h *AskRuleHandler) Handle(ctx context.Context, req *ToolCallRequest, tool IBaseTool) (*Decision, error) {
	for _, ask := range req.Context.AskTools {
		if MatchesTool(ask, req.ToolName) {
			return &Decision{
				Behavior: Ask,
				Reason:   fmt.Sprintf("tool %s requires user confirmation", req.ToolName),
			}, nil
		}
	}

	return h.propagate(ctx, req, tool, &Decision{Behavior: Next}, nil)
}

// ToolSelfCheckHandler 工具权限自检处理器,step 5
type ToolSelfCheckHandler struct {
	BaseHandler
}

func (h *ToolSelfCheckHandler) Handle(ctx context.Context, req *ToolCallRequest, tool IBaseTool) (*Decision, error) {
	d := tool.CheckPermission(ctx, req.Input, req.Context)
	return h.propagate(ctx, req, tool, d, nil)
}

// SafetyGuardrailHandler 抗绕过的安全护栏,step 6
type SafetyGuardrailHandler struct {
	BaseHandler
}

// 敏感路径前缀
var sensitivePathPrefixes = []string{".git/", ".vscode/", ".ssh/"}

func (h *SafetyGuardrailHandler) Handle(ctx context.Context, req *ToolCallRequest, tool IBaseTool) (*Decision, error) {
	// 检查文件路径是否命中敏感前缀
	if path, ok := req.Input["path"].(string); ok {
		for _, prefix := range sensitivePathPrefixes {
			if strings.HasPrefix(path, prefix) {
				return &Decision{
					Behavior: Ask,
					Reason:   fmt.Sprintf("access to sensitive path %s requires explicit approval", path),
				}, nil
			}
		}
	}

	return h.propagate(ctx, req, tool, &Decision{Behavior: Next}, nil)
}

// PermissionModeHandler 权限模式,step 7
type PermissionModeHandler struct {
	BaseHandler
}

func (h *PermissionModeHandler) Handle(ctx context.Context, req *ToolCallRequest, tool IBaseTool) (*Decision, error) {
	mode := req.Context.Permission

	switch mode {
	case PermissionModeBypass:
		// bypass 模式放行一切,但安全护栏已在上一阶段检查完成
		return &Decision{
			Behavior: Allow,
			Reason:   "permission bypass",
		}, nil
	case PermissionModePlan:
		// plan模式下只读工具放行,写入工具需要确认
		if tool.IsReadOnly(req.Input) {
			return &Decision{
				Behavior: Allow,
				Reason:   "read-only tool allowed in plan mode",
			}, nil
		}

		return &Decision{
			Behavior: Ask,
			Reason:   "write operation in plan mode requires confirmation",
		}, nil
	case PermissionModeAccept:
		if tool.IsReadOnly(req.Input) {
			return &Decision{
				Behavior: Allow,
				Reason:   "read only tool auto-approved in accept mode",
			}, nil
		}

		// 文件编辑类工具也自动放行
		return &Decision{
			Behavior: Allow,
			Reason:   "edit operation auto-approved in accept mode",
		}, nil
	default:
		return h.propagate(ctx, req, tool, &Decision{Behavior: Next}, nil)
	}
}

// AllowRuleHandler 允许规则处理器,step 8
// 匹配的工具调用自动放行,不弹出确认
type AllowRuleHandler struct {
	BaseHandler
}

func (h *AllowRuleHandler) Handle(ctx context.Context, req *ToolCallRequest, tool IBaseTool) (*Decision, error) {
	for _, allowed := range req.Context.AllowedTools {
		if MatchesTool(allowed, req.ToolName) {
			return &Decision{
				Behavior: Allow,
				Reason:   fmt.Sprintf("tool %s matched allow rule %s", req.ToolName, allowed),
			}, nil
		}
	}

	//只读工具默认放行
	if tool.IsReadOnly(req.Input) {
		return &Decision{
			Behavior: Allow,
			Reason:   "read only tool auto-approved by default",
		}, nil
	}

	return h.propagate(ctx, req, tool, &Decision{Behavior: Next}, nil)
}

// InteractiveConfirmHandler 链尾,当所有规则都为匹配时,用户确认,step 9
type InteractiveConfirmHandler struct {
	BaseHandler
}

func (h *InteractiveConfirmHandler) Handle(ctx context.Context, req *ToolCallRequest, tool IBaseTool) (*Decision, error) {
	if req.Context.UserConfirmFunc == nil {
		// 非交互式会话 -> fail-closed
		return &Decision{
			Behavior: Deny,
			Reason:   "interactive confirmation unavailable in non-interactive session",
		}, nil
	}

	approved, err := req.Context.UserConfirmFunc(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("interactive confirmation failed: %w", err)
	}

	if approved {
		return &Decision{
			Behavior: Allow,
			Reason:   "user approved",
		}, nil
	}

	return &Decision{
		Behavior: Deny,
		Reason:   "user denied",
	}, nil
}
