package toolexecutor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

type SeatbeltSandbox struct{}

func NewSeatbeltSandbox() *SeatbeltSandbox {
	return &SeatbeltSandbox{}
}

func (s *SeatbeltSandbox) Name() string {
	return "Seatbelt Sandbox"
}

func (s *SeatbeltSandbox) Available() bool {
	_, err := exec.LookPath("sandbox-exec")
	return err == nil
}

func (s *SeatbeltSandbox) Prepare(ctx context.Context, spec *SandBoxSpec, cmd *exec.Cmd) (*exec.Cmd, Cleanup, error) {
	if !s.Available() {
		return nil, nil, fmt.Errorf("seatbelt sandbox not available on this system")
	}

	profile := buildSeatbeltProfile(spec)

	args := []string{"-p", profile, cmd.Path}
	args = append(args, cmd.Args[1:]...)

	wrapped := exec.CommandContext(ctx, "sandbox-exec", args...)
	wrapped.Stdin = os.Stdin
	wrapped.Stdout = os.Stdout
	wrapped.Stderr = os.Stderr
	wrapped.Env = cmd.Env
	wrapped.Dir = spec.WorkingDir

	return wrapped, func() {}, nil
}
