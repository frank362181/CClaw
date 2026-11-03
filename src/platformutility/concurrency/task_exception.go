/**
* 并发模块的错误和意长定义
**/

package concurrency

import (
	"fmt"
	"strings"
	"time"

	"my.favorite/platformutility/apperr"
	pCommon "my.favorite/platformutility/common"
)

const (
	codeErrorTaskBase = iota + 800
	codeErrorTaskInvalidSpec
	codeErrorTaskManagerClosed
	codeErrorTaskNoCapacity
	codeErrorTaskDrainTimeout
	codeErrorTaskDuplicatedDaemon
	codeErrorTaskNoSlot
	codeErrorTaskOverRetries
)

const (
	txtErrorTaskInvalidSpec      = "task: Invalid Spec"
	txtErrorTaskManagerClosed    = "task: Manager Closed"
	txtErrorTaskNoCapacity       = "task: No Capacity"
	txtErrorTaskDrainTimeout     = "task: Drain Timeout"
	txtErrorTaskDuplicatedDaemon = "task: Duplicated Daemon name"
	txtErrorTaskNoSlot           = "task: No Slot"
	txtErrorTaskOverRetries      = "task: Over Retries"
)

var (
	ErrTaskInvalidSpec = apperr.Register(apperr.Kind{
		Name: "concurrency.TaskInvalidSpec",
		Code: codeErrorTaskInvalidSpec,
		Msg:  txtErrorTaskInvalidSpec,
	})
	ErrTaskManagerClosed = apperr.Register(apperr.Kind{
		Name: "concurrency.TaskManagerClosed",
		Code: codeErrorTaskManagerClosed,
		Msg:  txtErrorTaskManagerClosed,
	})

	ErrTaskNoCapacity = apperr.Register(apperr.Kind{
		Name: "concurrency.TaskNoCapacity",
		Code: codeErrorTaskNoCapacity,
		Msg:  txtErrorTaskNoCapacity,
	})

	ErrTaskDuplicatedDaemon = apperr.Register(apperr.Kind{
		Name: "concurrency.TaskDuplicatedDaemon",
		Code: codeErrorTaskDuplicatedDaemon,
		Msg:  txtErrorTaskDuplicatedDaemon,
	})

	ErrDrainTimeout = apperr.Register(apperr.Kind{
		Name: "concurrency.DrainTimeout",
		Code: codeErrorTaskDrainTimeout,
		Msg:  txtErrorTaskDrainTimeout,
	})

	ErrTaskNoSlot = apperr.Register(apperr.Kind{
		Name: "concurrency.TaskNoSlot",
		Code: codeErrorTaskNoSlot,
		Msg:  txtErrorTaskNoSlot,
	})

	ErrTaskOverRetries = apperr.Register(apperr.Kind{
		Name: "concurrency.TaskOverRetries",
		Code: codeErrorTaskOverRetries,
		Msg:  txtErrorTaskOverRetries,
	})
)

type DrainTimeoutError struct {
	Timeout time.Duration
	Scope   string // 空表示全部scope
	Running []RunningTask
}

func (e *DrainTimeoutError) Error() string {
	target := "all scopes"
	if strings.TrimSpace(e.Scope) != pCommon.BlankString {
		target = "scope=" + e.Scope
	}

	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "task: Drain timeout after %s (%s),%d task(s) still running",
		e.Timeout, target, len(e.Running))

	for _, rt := range e.Running {
		_, _ = fmt.Fprintf(&b, "\n %s/%s(id=%d) running %s",
			rt.Scope, rt.Name, rt.TaskId, time.Now().Sub(rt.Start))
	}

	return b.String()
}

func (e *DrainTimeoutError) Unwrap() error { return ErrDrainTimeout }
