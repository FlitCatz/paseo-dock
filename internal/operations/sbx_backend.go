package operations

import (
	"context"
	"errors"
	"fmt"
	"io"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/process"
)

type SBXBackend struct {
	Root, Memory string
	In           io.Reader
	Out, Err     io.Writer
}

var _ Backend = (*SBXBackend)(nil)

func validateSBXEntry(e config.Entry) error {
	if (e.EffectiveCategory() != config.CategorySandbox && e.EffectiveCategory() != config.CategoryLegacySandbox) || e.Sandbox == "" {
		return errors.New("SBX backend requires a sandbox identity")
	}
	return nil
}

func (s *Service) backend(category config.Category) (Backend, error) {
	if category == "" {
		category = config.CategoryLegacySandbox
	}
	if b := s.Backends[category]; b != nil {
		return b, nil
	}
	switch category {
	case config.CategorySandbox, config.CategoryLegacySandbox:
		return &SBXBackend{Root: s.Root, In: s.In, Out: s.Out, Err: s.Err}, nil
	case config.CategoryContainer:
		return &DockerBackend{Root: s.Root}, nil
	default:
		return nil, fmt.Errorf("unknown runtime category %q", category)
	}
}
func (b *SBXBackend) List(ctx context.Context) ([]RuntimeState, error) {
	s := Service{Root: b.Root}
	raw, _, err := s.observe(ctx, "sbx", "ls", "--json")
	if err != nil {
		return nil, err
	}
	rows, err := decodeRows[runtimeRow](raw, "sandboxes")
	if err != nil {
		return nil, err
	}
	states := []RuntimeState{}
	seen := map[string]bool{}
	for _, r := range rows {
		if r.Name == "" || seen[r.Name] {
			return nil, errors.New("invalid or duplicate sandbox status name")
		}
		seen[r.Name] = true
		states = append(states, RuntimeState{ID: r.Name, Running: runtimeState(r.Status) == "running", Known: runtimeState(r.Status) != "unknown"})
	}
	return states, nil
}
func (b *SBXBackend) Inspect(ctx context.Context, e config.Entry) (RuntimeState, error) {
	if err := validateSBXEntry(e); err != nil {
		return RuntimeState{}, err
	}
	rows, err := b.List(ctx)
	if err != nil {
		return RuntimeState{}, err
	}
	for _, r := range rows {
		if r.ID == e.Sandbox {
			return r, nil
		}
	}
	return RuntimeState{}, fmt.Errorf("sandbox %s missing", e.Sandbox)
}
func (b *SBXBackend) run(ctx context.Context, args []string, in io.Reader, out, stderr io.Writer) error {
	cmd := process.Command(ctx, "sbx", args...)
	cmd.Dir = b.Root
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return errors.Join(ctx.Err(), err)
	}
	return err
}
func (b *SBXBackend) Exec(ctx context.Context, e config.Entry, args []string, in io.Reader, out, stderr io.Writer) error {
	if err := validateSBXEntry(e); err != nil {
		return err
	}
	command := []string{"exec"}
	if e.EffectiveCategory() == config.CategorySandbox {
		command = append(command, "--env", "PATH="+config.SandboxAgentPath)
	}
	command = append(command, "--workdir", e.Workspace, e.Sandbox)
	return b.run(ctx, append(command, args...), in, out, stderr)
}
func (b *SBXBackend) Stop(ctx context.Context, e config.Entry) error {
	if err := validateSBXEntry(e); err != nil {
		return err
	}
	return b.run(ctx, []string{"stop", e.Sandbox}, b.In, b.Out, b.Err)
}
func (b *SBXBackend) Create(ctx context.Context, e config.Entry) (config.Entry, error) {
	if err := validateSBXEntry(e); err != nil {
		return e, err
	}
	memory := b.Memory
	if memory == "" {
		memory = "2g"
	}
	args := []string{"create", "--name", e.Sandbox, "--pull", "never", "--skills", "off", "--cpus", "2", "--memory", memory, "--template", e.Image}
	if e.Agent == "antigravity-cli" {
		args = append(args, "--env", "GEMINI_API_KEY=proxy-managed")
	}
	args = append(args, config.SandboxAgent(e.Agent), e.Workspace)
	return e, b.run(ctx, args, b.In, b.Out, b.Err)
}
