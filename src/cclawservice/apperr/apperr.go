// Package apperr 定义"应用错误"的统一模型:一类错误只在一处声明(Kind),
// 错误实例只承载可变信息(链路细节 + 底层原因)。
//
// 设计目标:
//  1. 单一事实来源:错误类别、对前端的业务码、HTTP 状态码、面向用户的文案(含建议动作)、
//     是否可重试、是否静默,全部挂在 Kind 上。不再出现"错误类型在 exception、
//     用户文案在 common、HTTP 状态在 controller"三处漂移的问题。
//  2. 构造即自解释:Kind.Newf(...) 生成的实例自带用户文案,上层无需再维护"错误→文案"映射表。
//  3. 兼容 errors.Is:实例与 Kind(或 Kind.Sentinel())比较的是 Kind 指针,
//     因此 errors.Is(err, ChatLLMAuthKind) 即可判定类别,并会沿 cause 链向上查找。
//  4. 尽早暴露配置错误:Kind 通过 Register 注册,重名/重码在包初始化阶段 panic,
//     而不是运行时静默取到错误的业务码或文案。

package apperr

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
)

// Kind 描述一类应用错误:它是什么、对外如何呈现、具备什么行为特征。
// Kind 由 Register 创建后即视为不可变(约定:注册后不再修改字段)。
type Kind struct {
	Name       string // 内部唯一标识(日志/排障用),如 "chat.llm.auth"
	Code       int    // 返回给前端的业务错误码(沿用既有 respCode 契约)
	HTTPStatus int    // 非流式响应对应的 HTTP 状态码;<=0 表示"不渲染/由调用方决定"
	Msg        string // 面向用户的文案(应包含可执行的建议动作)
	Retryable  bool   // 是否属于"瞬时故障、可安全重试"(重试/熔断策略据此决策)
	Silent     bool   // 是否静默:调用方不应再向前端输出该错误(如客户端已断开)
}

// String 便于日志/调试直接打印 Kind。
func (k *Kind) String() string {
	if k == nil {
		return "<nil>"
	}
	return k.Name
}

// Error 让 Kind 本身即可作为错误值使用(便于 errors.Is(err, SomeKind))。
// 其文本仅为类别名,不携带实例细节;正常的错误值仍是 Kind.New/Newf 生成的 *Error。
func (k *Kind) Error() string {
	return k.String()
}

// Sentinel 返回该类错误的哨兵实例(不含细节),便于 errors.Is(err, someKind.Sentinel())。
func (k *Kind) Sentinel() *Error {
	return &Error{kind: k}
}

// New 基于细节构造错误实例,并记录调用位置(便于日志定位)。
func (k *Kind) New(detail string) *Error {
	return k.construct(detail, 2)
}

// Newf 基于格式化细节构造错误实例。
func (k *Kind) Newf(format string, a ...interface{}) *Error {
	return k.construct(fmt.Sprintf(format, a...), 2)
}

// Wrap 构造错误实例并挂上底层原因(errors.Is/As 可继续向下透传)。
func (k *Kind) Wrap(cause error, detail string) *Error {
	return k.construct(detail, 3).WithCause(cause)
}

// WrapF 同 Wrap,细节支持格式化。
func (k *Kind) WrapF(cause error, format string, a ...interface{}) *Error {
	return k.construct(fmt.Sprintf(format, a...), 3).WithCause(cause)
}

// construct 统一构造入口:skip 为 runtime.Caller 的跳过层数(2 = 调用 New/Newf 的位置)。
func (k *Kind) construct(detail string, skip int) *Error {
	location := ""
	if _, file, line, ok := runtime.Caller(skip); ok {
		location = fmt.Sprintf("%s:%d", file, line)
	}
	return &Error{kind: k, detail: detail, location: location}
}

// Error 是一类 Kind 的具体错误实例。实例只承载可变信息(细节、产生位置、底层原因);
// 不可变元数据(用户文案/HTTP 状态/业务码)统一由 Kind 提供。
type Error struct {
	kind *Kind

	detail   string // 链路细节(日志用,不含敏感信息)
	location string // 产生位置 "file:line"
	cause    error  // 底层原因,支持 errors.Unwrap/Is/As 继续透传
}

// WithCause 附加底层原因并返回自身(便于链式构造)。
func (e *Error) WithCause(cause error) *Error {
	if e != nil {
		e.cause = cause
	}
	return e
}

// Kind 返回错误所属类别(可能为 nil)。
func (e *Error) Kind() *Kind {
	if e == nil {
		return nil
	}
	return e.kind
}

// Detail 返回链路细节(日志用)。
func (e *Error) Detail() string {
	if e == nil {
		return ""
	}
	return e.detail
}

// UserMsg 返回面向用户的文案(来自 Kind);无 Kind 时返回空串。
func (e *Error) UserMsg() string {
	if e == nil {
		return ""
	}
	return e.kind.Msg
}

// Retryable 返回该错误是否属于"可安全重试的瞬时故障"。
func (e *Error) Retryable() bool {
	if e == nil || e.kind == nil {
		return false
	}
	return e.kind.Retryable
}

// Unwrap 暴露底层原因,支持 errors.Is/As 沿链查找。
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Error 实现 error 接口:输出"类别名 + 业务码 + 产生位置 + 细节 + 底层原因",便于日志排障。
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	var sb strings.Builder
	sb.WriteString("ERROR: ")
	if e.kind != nil {
		sb.WriteString(e.kind.Name)
		if e.kind.Code != 0 {
			_, _ = fmt.Fprintf(&sb, "(%d)", e.kind.Code)
		}
	}
	if e.location != "" {
		sb.WriteString(" [")
		sb.WriteString(e.location)
		sb.WriteString("]")
	}
	if e.detail != "" {
		sb.WriteString(": ")
		sb.WriteString(e.detail)
	}
	if e.cause != nil {
		sb.WriteString(": ")
		sb.WriteString(e.cause.Error())
	}
	return sb.String()
}

// Is 支持 errors.Is(err, target):target 可以是同类别实例、Kind 指针或 Kind.Sentinel()。
func (e *Error) Is(target error) bool {
	if e == nil || e.kind == nil {
		return false
	}
	if t, ok := errors.AsType[*Error](target); ok {
		return t != nil && t.kind == e.kind
	}
	if t, ok := errors.AsType[*Kind](target); ok {
		return t != nil && t == e.kind
	}
	return false
}

var (
	registryMu sync.Mutex
	kindByName = map[string]*Kind{}
	kindByCode = map[int]*Kind{}
)

// Register 注册一个 Kind 并返回其指针。
// Name 必须非空且唯一;Code 非 0 时必须唯一。违反约束会在包初始化阶段 panic,
// 以便在启动时就暴露重复/错配的定义(而不是等到线上取错文案)。
func Register(k Kind) *Kind {
	if strings.TrimSpace(k.Name) == "" {
		panic("apperr: Kind.Name 不能为空")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if old, dup := kindByName[k.Name]; dup {
		panic(fmt.Sprintf("apperr: Kind 名称重复: %s(已注册 %s:%d)", k.Name, old.Name, old.Code))
	}
	if k.Code != 0 {
		if old, dup := kindByCode[k.Code]; dup {
			panic(fmt.Sprintf("apperr: Kind 业务码重复: %d(%s 与 %s)", k.Code, old.Name, k.Name))
		}
		kindByCode[k.Code] = &k
	}
	kindByName[k.Name] = &k
	return &k
}

// LookupByName 按 Name 查询已注册的 Kind(排障/生成文档用),未注册返回 nil。
func LookupByName(name string) *Kind {
	registryMu.Lock()
	defer registryMu.Unlock()
	return kindByName[name]
}

// LookupByCode 按业务码查询已注册的 Kind,未注册返回 nil。
func LookupByCode(code int) *Kind {
	registryMu.Lock()
	defer registryMu.Unlock()
	return kindByCode[code]
}

// As 返回错误链上第一个应用错误;不存在时返回 nil。
func As(err error) *Error {
	if err == nil {
		return nil
	}
	if target, ok := errors.AsType[*Error](err); ok {
		return target
	}
	return nil
}

// Of 返回错误链上第一个应用错误所属的 Kind;不存在时返回 nil。
// 兼容两种用法:正常错误实例(*Error),或直接把 Kind 作为错误值传递。
func Of(err error) *Kind {
	if err == nil {
		return nil
	}
	if kind, ok := errors.AsType[*Kind](err); ok {
		return kind
	}
	return As(err).Kind()
}

// UserMsg 返回错误链上第一个应用错误的用户文案;非应用错误返回空串。
func UserMsg(err error) string {
	if kind := Of(err); kind != nil {
		return kind.Msg
	}
	return ""
}

// Retryable 返回错误链上第一个应用错误是否可安全重试。
func Retryable(err error) bool {
	if kind := Of(err); kind != nil {
		return kind.Retryable
	}
	return false
}
