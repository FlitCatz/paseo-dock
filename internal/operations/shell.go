package operations

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"paseo-dock/internal/config"
	"paseo-dock/internal/operationlock"
)

// ShellProcess transfers terminal streams to an interactive child. Run owns the
// full target lock and process lifetime; callers cannot launch an unlocked Cmd.
// Its interface also satisfies Bubble Tea's ExecCommand terminal handoff.
type ShellProcess interface {
	Run() error
	SetStdin(io.Reader)
	SetStdout(io.Writer)
	SetStderr(io.Writer)
}

type shellProcess struct {
	service     *Service
	ctx         context.Context
	target      Target
	in          io.Reader
	out, stderr io.Writer
}

func (p *shellProcess) SetStdin(in io.Reader)      { p.in = in }
func (p *shellProcess) SetStdout(out io.Writer)    { p.out = out }
func (p *shellProcess) SetStderr(stderr io.Writer) { p.stderr = stderr }
func (p *shellProcess) Run() error {
	release, err := operationlock.Acquire(p.ctx, p.target.Runtime, []string{operationlock.EntryKey(p.target.Entry)})
	if err != nil {
		return err
	}
	defer release()
	target, err := p.service.resolveTarget(p.target)
	if err != nil {
		return err
	}
	var cmd *exec.Cmd
	if target.Category == config.CategoryContainer {
		backend, err := p.service.backend(target.Category)
		if err != nil {
			return err
		}
		state, err := backend.Inspect(p.ctx, target.Entry)
		if err != nil {
			return err
		}
		if !state.Running {
			return errors.New("Docker container is stopped; start it explicitly before shell")
		}
		user, err := hostDockerUser()
		if err != nil {
			return err
		}
		cmd = exec.CommandContext(p.ctx, "docker", "exec", "-it", "--user", user, "--env", "HOME=/home/agent", "--workdir", target.Workspace, target.Entry.ContainerID, "bash")
	} else {
		cmd = exec.CommandContext(p.ctx, "sbx", "exec", "-it", "--workdir", target.Workspace, target.Entry.Sandbox, "bash")
	}
	// Keep the terminal process group: Bubble Tea releases its terminal before
	// Run and reclaims it only after this child has exited and the lock is released.
	cmd.Dir = p.service.Root
	cmd.Stdin = p.in
	cmd.Stdout = p.out
	cmd.Stderr = p.stderr
	err = cmd.Run()
	if p.ctx.Err() != nil {
		return errors.Join(p.ctx.Err(), err)
	}
	return err
}
