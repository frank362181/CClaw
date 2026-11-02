package toolexecutor

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	pCommon "my.favorite/platformutility/common"
)

const ProcessName = "sandbox-exec"

type SeatbeltSandbox struct{}

func NewSeatbeltSandbox() *SeatbeltSandbox {
	return &SeatbeltSandbox{}
}

func (s *SeatbeltSandbox) Name() string {
	return "Seatbelt Sandbox"
}

func (s *SeatbeltSandbox) Available() bool {
	_, err := exec.LookPath(ProcessName)
	return err == nil
}

func (s *SeatbeltSandbox) Prepare(ctx context.Context, spec *SandBoxSpec, cmd *exec.Cmd) (*exec.Cmd, Cleanup, error) {
	if !s.Available() {
		return nil, nil, fmt.Errorf("seatbelt sandbox not available on this system")
	}

	profile := buildSeatbeltProfile(spec)

	args := make([]string, 0, len(cmd.Args)+3)
	args = append(args, "-p", profile, cmd.Path)
	if len(cmd.Args) > 1 {
		args = append(args, cmd.Args[1:]...)
	}

	wrapped := exec.CommandContext(ctx, ProcessName, args...)

	// 必须继承原 cmd的stdio等
	wrapped.Stdin = cmd.Stdin
	wrapped.Stdout = cmd.Stdout
	wrapped.Stderr = cmd.Stderr
	wrapped.Env = cmd.Env
	wrapped.Dir = cmd.Dir
	if wrapped.Dir == pCommon.BlankString {
		wrapped.Dir = spec.WorkingDir
	}

	// 设置进程组:否则超时只会kill ${ProcessName},它fork出来的进程成为orphan process且继续运行
	pCommon.SetProcessGroup(wrapped)
	wrapped.Cancel = func() error {
		pCommon.KillProcessGroup(wrapped)
		return wrapped.Wait()
	}
	wrapped.WaitDelay = time.Duration(5) * time.Second //进程杀死等待的最大时间长度

	return wrapped, func() {}, nil
}
