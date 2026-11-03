/**
* apperr 错误模型的契约测试。
*
* 重点锁定"注册期管理"的约束(重名/重码必须在启动期 panic,而不是运行时静默取错文案)
* 与错误实例的行为(类别判定、cause 链路、用户文案、nil 安全)。
 */

package apperr

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// 测试专用 Kind:使用独立命名空间,避免与业务 Kind 的名称/业务码冲突。
var (
	testKindA = Register(Kind{
		Name:       "test.apperr.a",
		Code:       910001,
		HTTPStatus: 418,
		Msg:        "A 类错误,请稍后重试",
		Retryable:  true,
	})
	testKindB = Register(Kind{
		Name:       "test.apperr.b",
		Code:       910002,
		HTTPStatus: 400,
		Msg:        "B 类错误",
		Silent:     true,
	})
)

// assertPanics 断言 fn 会 panic(注册期约束靠它拦截)。
func assertPanics(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatalf("%s: 应当 panic", name)
		}
	}()
	fn()
}

// TestRegisterRejectsDuplicateName 重名必须在注册时暴露,否则日志/排障无法区分类别。
func TestRegisterRejectsDuplicateName(t *testing.T) {
	assertPanics(t, "重复 Name", func() {
		Register(Kind{Name: testKindA.Name, Code: 919999})
	})
}

// TestRegisterRejectsDuplicateCode 重码必须暴露:业务码是前端契约,同码会导致前端分错支。
func TestRegisterRejectsDuplicateCode(t *testing.T) {
	assertPanics(t, "重复 Code", func() {
		Register(Kind{Name: "test.apperr.dup_code", Code: testKindA.Code})
	})
}

// TestRegisterRejectsEmptyName 名称是类别主键,不允许为空/空白。
func TestRegisterRejectsEmptyName(t *testing.T) {
	assertPanics(t, "空白 Name", func() {
		Register(Kind{Name: "   ", Code: 919998})
	})
	assertPanics(t, "空 Name", func() {
		Register(Kind{Code: 919997})
	})
}

// TestRegisterAllowsMultipleZeroCode 约定:Code==0 表示"无业务码"(内部错误),
// 可以有多个,且不进业务码索引(避免 LookupByCode(0) 命中任意一个)。
func TestRegisterAllowsMultipleZeroCode(t *testing.T) {
	a := Register(Kind{Name: "test.apperr.zero_a", Msg: "a"})
	b := Register(Kind{Name: "test.apperr.zero_b", Msg: "b"})
	if a.Code != 0 || b.Code != 0 {
		t.Fatalf("Code 应为 0: %d / %d", a.Code, b.Code)
	}
	if got := LookupByCode(0); got != nil {
		t.Fatalf("Code=0 不应进业务码索引,实际命中 %v", got)
	}
	if got := LookupByName("test.apperr.zero_a"); got != a {
		t.Fatalf("按名称应能查到: %v", got)
	}
}

func TestLookup(t *testing.T) {
	if got := LookupByName(testKindA.Name); got != testKindA {
		t.Fatalf("LookupByName 未命中: %v", got)
	}
	if got := LookupByCode(testKindB.Code); got != testKindB {
		t.Fatalf("LookupByCode 未命中: %v", got)
	}
	if got := LookupByName("test.apperr.not.exist"); got != nil {
		t.Fatalf("未注册名称应返回 nil,实际 %v", got)
	}
	if got := LookupByCode(-1); got != nil {
		t.Fatalf("未注册业务码应返回 nil,实际 %v", got)
	}
}

// TestKindIsErrorValue Kind 本身可作为错误值参与 errors.Is。
func TestKindIsErrorValue(t *testing.T) {
	var k error = testKindA
	if k.Error() != "test.apperr.a" {
		t.Fatalf("Kind.Error() 应为类别名,实际 %q", k.Error())
	}
	if testKindA.String() != "test.apperr.a" {
		t.Fatalf("Kind.String() 应为类别名")
	}
	var nilKind *Kind
	if nilKind.String() != "<nil>" || nilKind.Error() != "<nil>" {
		t.Fatalf("nil Kind 应安全返回 <nil>")
	}
}

func TestNewCarriesKindMetadata(t *testing.T) {
	err := testKindA.New("detail-1")
	if err.Kind() != testKindA {
		t.Fatalf("Kind 未挂载")
	}
	if err.Detail() != "detail-1" {
		t.Fatalf("Detail 丢失: %q", err.Detail())
	}
	if err.UserMsg() != testKindA.Msg {
		t.Fatalf("UserMsg 应取自 Kind: %q", err.UserMsg())
	}
	if !err.Retryable() {
		t.Fatalf("Retryable 应继承 Kind")
	}
	// 日志文本需含:类别名 + 业务码 + 产生位置 + 细节
	text := err.Error()
	for _, want := range []string{testKindA.Name, "910001", "apperr_test.go", "detail-1"} {
		if !strings.Contains(text, want) {
			t.Errorf("Error() 缺少 %q: %s", want, text)
		}
	}
}

// TestErrorTextOmitsZeroCode 无业务码的内部错误不应在日志里显示 (0)。
func TestErrorTextOmitsZeroCode(t *testing.T) {
	kind := Register(Kind{Name: "test.apperr.no_code", Msg: "内部错误"})
	if strings.Contains(kind.New("boom").Error(), "(0)") {
		t.Fatalf("Code=0 不应渲染为 (0): %s", kind.New("boom").Error())
	}
}

func TestNewfFormatsDetail(t *testing.T) {
	err := testKindA.Newf("agent %s busy, max %d", "a1", 3)
	if err.Detail() != "agent a1 busy, max 3" {
		t.Fatalf("Newf 未格式化: %q", err.Detail())
	}
}

// TestErrorsIsComparesKind 实例与 Kind / Kind.Sentinel() 等价,且沿包装链向上查找。
func TestErrorsIsComparesKind(t *testing.T) {
	err := testKindA.New("x")
	if !errors.Is(err, testKindA) {
		t.Fatalf("errors.Is(err, Kind) 应为真")
	}
	if !errors.Is(err, testKindA.Sentinel()) {
		t.Fatalf("errors.Is(err, Kind.Sentinel()) 应为真")
	}
	if !errors.Is(err, testKindA.New("y")) {
		t.Fatalf("同类别实例应相等(按 Kind 指针)")
	}
	if errors.Is(err, testKindB) {
		t.Fatalf("不同 Kind 不应相等")
	}
	wrapped := fmt.Errorf("调用链包装: %w", err)
	if !errors.Is(wrapped, testKindA) {
		t.Fatalf("包装后应仍能识别类别")
	}
}

// TestWrapKeepsCause 挂上的底层原因必须可继续透传(errors.Is/As 排障用)。
func TestWrapKeepsCause(t *testing.T) {
	cause := errors.New("upstream timeout")
	err := testKindA.Wrap(cause, "ctx")
	if !errors.Is(err, cause) {
		t.Fatalf("Wrap 的 cause 未透传")
	}
	if got := err.Unwrap(); got != cause {
		t.Fatalf("Unwrap 应返回原始 cause")
	}
	if err.Detail() != "ctx" {
		t.Fatalf("Wrap 细节丢失: %q", err.Detail())
	}

	err2 := testKindA.WrapF(cause, "ctx %d", 7)
	if err2.Detail() != "ctx 7" {
		t.Fatalf("WrapF 未格式化: %q", err2.Detail())
	}
	if !errors.Is(err2, cause) {
		t.Fatalf("WrapF 的 cause 未透传")
	}
	if testKindA.New("c").WithCause(cause).Unwrap() != cause {
		t.Fatalf("WithCause 未生效")
	}
}

func TestOfUserMsgRetryableAcrossChain(t *testing.T) {
	err := testKindA.Wrap(errors.New("boom"), "detail")
	wrapped := fmt.Errorf("outer: %w", err)

	if got := Of(wrapped); got != testKindA {
		t.Fatalf("Of 应沿链找到 Kind,实际 %v", got)
	}
	if got := UserMsg(wrapped); got != testKindA.Msg {
		t.Fatalf("UserMsg 应为 Kind.Msg,实际 %q", got)
	}
	if !Retryable(wrapped) {
		t.Fatalf("Retryable 应为真")
	}
	if As(wrapped) == nil {
		t.Fatalf("As 应沿链找到应用错误")
	}
	// 兼容"直接把 Kind 当错误值传递"
	if Of(testKindB) != testKindB {
		t.Fatalf("Of 应兼容裸 Kind")
	}
	if got := UserMsg(error(testKindB)); got != testKindB.Msg {
		t.Fatalf("UserMsg 应兼容裸 Kind,实际 %q", got)
	}
}

func TestOfAndUserMsgOnPlainError(t *testing.T) {
	plain := errors.New("plain")
	if Of(plain) != nil {
		t.Fatalf("非应用错误应返回 nil Kind")
	}
	if As(plain) != nil {
		t.Fatalf("非应用错误 As 应返回 nil")
	}
	if UserMsg(plain) != "" {
		t.Fatalf("非应用错误应返回空文案")
	}
	if Retryable(plain) {
		t.Fatalf("非应用错误不应可重试")
	}
}

func TestNilSafety(t *testing.T) {
	if Of(nil) != nil || As(nil) != nil || UserMsg(nil) != "" || Retryable(nil) {
		t.Fatalf("nil 错误应安全返回零值")
	}

	var err *Error
	if err.Kind() != nil || err.Detail() != "" || err.UserMsg() != "" || err.Retryable() {
		t.Fatalf("nil 接收者访问器应返回零值")
	}
	if err.Unwrap() != nil {
		t.Fatalf("nil 接收者 Unwrap 应为 nil")
	}
	if err.Error() == "" {
		t.Fatalf("nil 接收者 Error() 不应为空串")
	}
	if err.Is(testKindA) {
		t.Fatalf("nil 接收者不应匹配任何 Kind")
	}
	if err.WithCause(errors.New("x")) != nil {
		t.Fatalf("nil 接收者 WithCause 应返回 nil")
	}
}

// TestSilentMetadata Silent 用于调用方判断"不该再向前端输出"(如客户端已断开)。
func TestSilentMetadata(t *testing.T) {
	if !testKindB.Silent {
		t.Fatalf("Silent 未生效")
	}
	if testKindA.Silent {
		t.Fatalf("默认不应为 Silent")
	}
	if testKindA.HTTPStatus != 418 || testKindB.HTTPStatus != 400 {
		t.Fatalf("HTTPStatus 元数据丢失")
	}
}

// TestNotifyMetadata Notify 用于调用方判断"必须醒目告知用户"(如欠费/密钥无效)。
func TestNotifyMetadata(t *testing.T) {
	kind := Register(Kind{Name: "test.apperr.notify", Msg: "模型服务额度不足,请充值后重试", Notify: true})

	if !Notify(kind.New("quota exhausted")) {
		t.Fatalf("Notify 未生效")
	}
	if !Notify(kind) { // Kind 本身作为错误值
		t.Fatalf("Kind 作为错误值时应命中 Notify")
	}
	if Notify(testKindA.New("boom")) {
		t.Fatalf("默认不应为 Notify")
	}
	if Notify(nil) || Notify(errors.New("plain")) {
		t.Fatalf("nil 与非应用错误应为 false")
	}
}

// TestRegisterRejectsSilentAndNotify 静默("不输出")与显式提醒("必须输出")语义冲突,
// 必须在注册期拦截而不是运行时表现矛盾。
func TestRegisterRejectsSilentAndNotify(t *testing.T) {
	assertPanics(t, "Silent 与 Notify 同时为真", func() {
		Register(Kind{Name: "test.apperr.silent_and_notify", Silent: true, Notify: true})
	})
}
