package operations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"paseo-dock/internal/config"
	"paseo-dock/internal/containerpolicy"
	"paseo-dock/internal/process"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// io.paseo-dock.* labels are on existing containers and stay after the rename (see config/legacy.go).
const DockerManagedLabel = "io.paseo-dock.managed"
const dockerPolicyLabel = "io.paseo-dock.security-policy"
const dockerPolicyDigestLabel = "io.paseo-dock.security-policy-digest"

var dockerExactID = regexp.MustCompile(`^[a-f0-9]{64}$`)

// DockerBackend operates only on project-managed, fully inspected identities.
// Root is the build/command directory; it is never mounted into a guest.
type DockerBackend struct{ Root string }

var _ Backend = (*DockerBackend)(nil)

type dockerInspection struct {
	ID         string `json:"Id"`
	Image      string
	State      struct{ Running bool }
	Config     struct{ Labels map[string]string }
	Mounts     []RuntimeMount
	HostConfig struct {
		SecurityOpt  []string
		CapAdd       []string
		CapDrop      []string
		Privileged   bool
		PidMode      string
		IpcMode      string
		NetworkMode  string
		PortBindings map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string
		}
	}
	NetworkSettings struct{ Networks map[string]json.RawMessage }
}

func (b *DockerBackend) capture(ctx context.Context, args ...string) ([]byte, error) {
	cmd := process.Command(ctx, "docker", args...)
	cmd.Dir = b.Root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if len(args) == 4 && args[0] == "inspect" && args[1] == "--type" && args[2] == "container" {
			message := strings.TrimSpace(stderr.String())
			for _, prefix := range []string{"Error: No such object: ", "Error: No such container: ", "Error response from daemon: No such container: "} {
				if message == prefix+args[3] {
					return out, fmt.Errorf("container %s: %w", args[3], ErrRuntimeNotFound)
				}
			}
		}
		return out, fmt.Errorf("docker %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
func (b *DockerBackend) inspectID(ctx context.Context, id string) (dockerInspection, error) {
	var result dockerInspection
	if !dockerExactID.MatchString(id) {
		return result, fmt.Errorf("Docker target requires an exact container ID")
	}
	out, err := b.capture(ctx, "inspect", "--type", "container", id)
	if err != nil {
		return result, err
	}
	var values []dockerInspection
	if err := json.Unmarshal(out, &values); err != nil {
		return result, fmt.Errorf("decode Docker inspect: %w", err)
	}
	if len(values) != 1 {
		return result, fmt.Errorf("Docker inspect returned %d containers", len(values))
	}
	result = values[0]
	if result.ID != id || result.Config.Labels[DockerManagedLabel] != "true" || result.Config.Labels["io.paseo-dock.category"] != "container" {
		return dockerInspection{}, fmt.Errorf("Docker container identity or management label mismatch")
	}
	return result, nil
}
func observedDockerState(raw dockerInspection) (RuntimeState, error) {
	state := RuntimeState{ID: raw.ID, ImageID: raw.Image, Running: raw.State.Running, Mounts: raw.Mounts, Network: raw.HostConfig.NetworkMode}
	for key, bindings := range raw.HostConfig.PortBindings {
		port, proto, ok := strings.Cut(key, "/")
		if !ok || proto != "tcp" {
			return RuntimeState{}, fmt.Errorf("unsupported Docker port %s", key)
		}
		container, err := strconv.Atoi(port)
		if err != nil || container < 1 || container > 65535 {
			return RuntimeState{}, fmt.Errorf("invalid Docker container port")
		}
		for _, binding := range bindings {
			host, err := strconv.Atoi(binding.HostPort)
			if err != nil || host < 1 || host > 65535 {
				return RuntimeState{}, fmt.Errorf("invalid Docker host port")
			}
			state.Ports = append(state.Ports, config.PortMapping{HostAddress: binding.HostIP, HostPort: host, ContainerPort: container})
		}
	}
	state.Known = true
	return state, nil
}
func validateDockerEntry(e config.Entry, creating bool, allowMissingWorkspace ...bool) (config.Entry, error) {
	if e.SecurityPolicy == "" {
		if e.SecurityPolicyDigest != "" {
			return e, fmt.Errorf("Docker policy digest without policy")
		}
	} else if e.SecurityPolicy != containerpolicy.ID || e.SecurityPolicyDigest != containerpolicy.SHA256 || !slices.Contains(e.AgentIDs(), "codex") {
		return e, fmt.Errorf("unsupported Docker security policy or digest")
	}
	if e.Category != config.CategoryContainer {
		return e, fmt.Errorf("Docker backend requires container category")
	}
	workspace, err := config.Canonical(e.Workspace)
	if err != nil {
		return e, err
	}
	info, statErr := os.Stat(workspace)
	missingAllowed := len(allowMissingWorkspace) > 0 && allowMissingWorkspace[0] && os.IsNotExist(statErr)
	if !missingAllowed && (statErr != nil || !info.IsDir()) {
		return e, fmt.Errorf("Docker workspace must be an existing directory: %s", workspace)
	}
	if strings.ContainsAny(workspace, ",\n\r") {
		return e, fmt.Errorf("workspace cannot be represented safely in a Docker bind mount")
	}
	if !creating && workspace != e.Workspace {
		return e, fmt.Errorf("Docker workspace is not canonical")
	}
	e.Workspace = workspace
	if creating && e.Network == "" {
		e.Network = "bridge"
	}
	if e.Network != "bridge" {
		return e, fmt.Errorf("Docker network must be bridge")
	}
	e.Ports = slices.Clone(e.Ports)
	seenHost := map[int]bool{}
	seenContainer := map[int]bool{}
	for i, p := range e.Ports {
		if creating && p.HostAddress == "" {
			p.HostAddress = "127.0.0.1"
		}
		if p.HostAddress != "127.0.0.1" || p.HostPort < 1 || p.HostPort > 65535 || p.ContainerPort < 1 || p.ContainerPort > 65535 || seenHost[p.HostPort] || seenContainer[p.ContainerPort] {
			return e, fmt.Errorf("Docker ports require unique explicit loopback TCP mappings")
		}
		seenHost[p.HostPort] = true
		seenContainer[p.ContainerPort] = true
		e.Ports[i] = p
	}
	if !creating && (!dockerExactID.MatchString(e.ContainerID) || e.ImageID == "") {
		return e, fmt.Errorf("Docker target requires exact container and image IDs")
	}
	return e, nil
}
func (b *DockerBackend) Inspect(ctx context.Context, e config.Entry) (RuntimeState, error) {
	e, err := validateDockerEntry(e, false, true)
	if err != nil {
		return RuntimeState{}, err
	}
	raw, err := b.inspectID(ctx, e.ContainerID)
	if err != nil {
		return RuntimeState{}, err
	}
	if err := validateDockerPolicy(e, raw); err != nil {
		return RuntimeState{}, err
	}
	state, err := observedDockerState(raw)
	if err != nil {
		return RuntimeState{}, err
	}
	if state.ImageID != e.ImageID || state.Network != e.Network {
		return RuntimeState{}, fmt.Errorf("Docker image or network mismatch; recreation requires an explicit action")
	}
	// Docker can clear endpoint attachments while stopped; a running container
	// must have exactly its saved bridge attachment, never zero attachments.
	bridge := bytes.TrimSpace(raw.NetworkSettings.Networks["bridge"])
	if (state.Running || len(raw.NetworkSettings.Networks) > 0) && (len(raw.NetworkSettings.Networks) != 1 || len(bridge) == 0 || bridge[0] != '{') {
		return RuntimeState{}, fmt.Errorf("Docker attached networks mismatch")
	}
	if len(state.Mounts) != 1 {
		return RuntimeState{}, fmt.Errorf("Docker workspace mount count mismatch")
	}
	mount := state.Mounts[0]
	if mount.Type != "bind" || mount.Source != e.Workspace || mount.Destination != e.Workspace || !mount.RW {
		return RuntimeState{}, fmt.Errorf("Docker workspace mount mismatch")
	}
	actual := e
	actual.Ports = state.Ports
	if !e.EqualIdentity(actual) {
		return RuntimeState{}, fmt.Errorf("Docker port mappings mismatch")
	}
	return state, nil
}

func validateDockerPolicy(e config.Entry, raw dockerInspection) error {
	if raw.Config.Labels[dockerPolicyLabel] != e.SecurityPolicy || raw.Config.Labels[dockerPolicyDigestLabel] != e.SecurityPolicyDigest {
		return fmt.Errorf("Docker security policy labels mismatch")
	}
	if e.SecurityPolicy == "" {
		// Default creation never grants capabilities or shares host namespaces.
		if raw.HostConfig.Privileged || len(raw.HostConfig.CapAdd) != 0 {
			return fmt.Errorf("Docker default security capabilities mismatch: privileged mode and added capabilities are forbidden")
		}
		// Older containers inherited daemon IPC defaults. Shareable still owns
		// its namespace; only host/container modes join an outside namespace.
		ownIPC := slices.Contains([]string{"", "private", "shareable", "none"}, raw.HostConfig.IpcMode)
		if raw.HostConfig.PidMode != "" || !ownIPC || raw.HostConfig.NetworkMode != "bridge" {
			return fmt.Errorf("Docker default security namespaces mismatch")
		}
		return nil
	}
	// Docker may persist the canonical CAP_ name rather than the CLI spelling.
	dropsSysAdmin := slices.Contains(raw.HostConfig.CapDrop, "SYS_ADMIN") || slices.Contains(raw.HostConfig.CapDrop, "CAP_SYS_ADMIN")
	if raw.HostConfig.Privileged || len(raw.HostConfig.CapAdd) != 0 || !dropsSysAdmin {
		return fmt.Errorf("Docker security policy capabilities mismatch")
	}
	if len(raw.HostConfig.SecurityOpt) != 2 {
		return fmt.Errorf("Docker security options mismatch")
	}
	nnp, seccomp := false, false
	for _, opt := range raw.HostConfig.SecurityOpt {
		switch {
		case opt == "no-new-privileges" || opt == "no-new-privileges=true":
			if nnp {
				return fmt.Errorf("duplicate Docker no-new-privileges option")
			}
			nnp = true
		case strings.HasPrefix(opt, "seccomp="):
			if seccomp || !containerpolicy.MatchesJSON([]byte(strings.TrimPrefix(opt, "seccomp="))) {
				return fmt.Errorf("Docker seccomp profile mismatch")
			}
			seccomp = true
		default:
			return fmt.Errorf("unexpected Docker security option")
		}
	}
	if !nnp || !seccomp {
		return fmt.Errorf("Docker security policy missing required options")
	}
	return nil
}
func (b *DockerBackend) List(ctx context.Context) ([]RuntimeState, error) {
	out, err := b.capture(ctx, "ps", "--all", "--no-trunc", "--filter", "label="+DockerManagedLabel+"=true", "--filter", "label=io.paseo-dock.category=container", "--format", "{{.ID}}")
	if err != nil {
		return nil, err
	}
	states := []RuntimeState{}
	for _, id := range strings.Fields(string(out)) {
		raw, err := b.inspectID(ctx, id)
		if err != nil {
			return nil, err
		}
		state, err := observedDockerState(raw)
		if err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	return states, nil
}
func hostDockerUser() (string, error) {
	uid, gid := os.Getuid(), os.Getgid()
	if uid <= 0 || gid <= 0 {
		return "", fmt.Errorf("Docker agent requires a non-root host UID and GID")
	}
	return fmt.Sprintf("%d:%d", uid, gid), nil
}
func (b *DockerBackend) Exec(ctx context.Context, e config.Entry, args []string, in io.Reader, out, stderr io.Writer) error {
	state, err := b.Inspect(ctx, e)
	if err != nil {
		return err
	}
	if !state.Running {
		return fmt.Errorf("Docker container is stopped; start it explicitly before exec")
	}
	if len(args) == 0 {
		return fmt.Errorf("Docker exec command is required")
	}
	user, err := hostDockerUser()
	if err != nil {
		return err
	}
	command := []string{"exec", "-i", "--user", user, "--env", "HOME=/home/agent", "--workdir", e.Workspace, e.ContainerID}
	command = append(command, args...)
	cmd := process.Command(ctx, "docker", command...)
	cmd.Dir = b.Root
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = stderr
	err = cmd.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
func (b *DockerBackend) Stop(ctx context.Context, e config.Entry) error {
	if _, err := b.Inspect(ctx, e); err != nil {
		return err
	}
	_, err := b.capture(ctx, "stop", e.ContainerID)
	return err
}
func (b *DockerBackend) Remove(ctx context.Context, e config.Entry) error {
	state, err := b.Inspect(ctx, e)
	if err != nil {
		return err
	}
	if !state.Known {
		return errors.New("container state unknown; removal refused")
	}
	if state.Running {
		return errors.New("container is running; stop first")
	}
	_, err = b.capture(ctx, "rm", e.ContainerID)
	return err
}
func (b *DockerBackend) ready(ctx context.Context, e config.Entry, user string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ids := strings.Split(user, ":")
	probe := fmt.Sprintf(`test -f /tmp/paseo-dock-agent-ready && test "$(id -u)" = %s && test "$(id -g)" = %s && test -w "$HOME" && test -w .`, ids[0], ids[1])
	var last error
	for {
		if _, err := b.Inspect(ctx, e); err != nil {
			return fmt.Errorf("Docker startup identity verification failed: %w", err)
		}
		if _, last = b.capture(ctx, "exec", "--user", user, "--env", "HOME=/home/agent", "--workdir", e.Workspace, e.ContainerID, "sh", "-c", probe); last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("Docker agent startup verification failed: %w", last)
		case <-time.After(100 * time.Millisecond):
		}
	}
}
func (b *DockerBackend) Create(ctx context.Context, e config.Entry) (config.Entry, error) {
	var err error
	e, err = validateDockerEntry(e, e.ContainerID == "")
	if err != nil {
		return e, err
	}
	user, err := hostDockerUser()
	if err != nil {
		return e, err
	}
	if e.ContainerID != "" {
		state, err := b.Inspect(ctx, e)
		if err != nil {
			return e, err
		}
		if !state.Running {
			if _, err := b.capture(ctx, "start", e.ContainerID); err != nil {
				return e, err
			}
		}
		if _, err := b.Inspect(ctx, e); err != nil {
			return e, err
		}
		return e, b.ready(ctx, e, user)
	}
	if e.Image == "" {
		return e, fmt.Errorf("Docker image is required")
	}
	image, err := b.capture(ctx, "image", "inspect", e.Image)
	if err != nil {
		return e, err
	}
	var images []struct {
		ID           string `json:"Id"`
		Architecture string
	}
	if err := json.Unmarshal(image, &images); err != nil {
		return e, err
	}
	if len(images) != 1 || images[0].ID == "" {
		return e, fmt.Errorf("Docker image ID missing")
	}
	if e.ImageID != "" && e.ImageID != images[0].ID {
		return e, fmt.Errorf("Docker image ID changed")
	}
	e.ImageID = images[0].ID
	if e.SecurityPolicy != "" && images[0].Architecture != "arm64" && images[0].Architecture != "amd64" {
		return e, fmt.Errorf("Codex user namespace policy requires arm64 or amd64 image")
	}
	// Docker writes the CID before starting the container. A private directory
	// keeps that identity recoverable even when run fails or is canceled later.
	cidDir, err := os.MkdirTemp("", "paseo-dock-docker-cid-")
	if err != nil {
		return e, err
	}
	defer os.RemoveAll(cidDir)
	cidPath := filepath.Join(cidDir, "container-id")
	sum := sha256.Sum256([]byte(e.Workspace + "\x00" + e.Key()))
	args := []string{"run", "--detach", "--name", fmt.Sprintf("paseo-dock-%x", sum[:12]), "--label", DockerManagedLabel + "=true", "--label", "io.paseo-dock.category=container", "--network", "bridge", "--ipc", "private", "--mount", "type=bind,src=" + e.Workspace + ",dst=" + e.Workspace, "--workdir", e.Workspace, "--user", "root", "--env", "HOME=/home/agent", "--entrypoint", "/bin/sh"}
	if e.SecurityPolicy != "" {
		path, cleanup, err := containerpolicy.WriteTemp()
		if err != nil {
			return e, err
		}
		defer cleanup()
		args = append(args, "--label", dockerPolicyLabel+"="+e.SecurityPolicy, "--label", dockerPolicyDigestLabel+"="+e.SecurityPolicyDigest, "--security-opt", "seccomp="+path, "--security-opt", "no-new-privileges", "--cap-drop", "SYS_ADMIN")
	}
	args = append(args, "--cidfile", cidPath)
	for _, p := range e.Ports {
		args = append(args, "--publish", fmt.Sprintf("127.0.0.1:%d:%d/tcp", p.HostPort, p.ContainerPort))
	}
	ids := strings.Split(user, ":")
	// Root only aligns permissions. The persistent main process and all later
	// exec commands use the host-aligned agent user.
	bootstrap := fmt.Sprintf(`set -eu; /usr/bin/rm -f /tmp/paseo-dock-agent-ready; /usr/sbin/groupmod -o -g %s "$(/usr/bin/id -gn agent)"; /usr/sbin/usermod -o -u %s -g %s agent; /usr/bin/chown -R %s /home/agent; /usr/bin/touch /tmp/paseo-dock-agent-ready; exec /usr/sbin/runuser -u agent -- /usr/bin/sleep infinity`, ids[1], ids[0], ids[1], user)
	args = append(args, e.ImageID, "-c", bootstrap)
	out, err := b.capture(ctx, args...)
	cid, _ := os.ReadFile(cidPath)
	e.ContainerID = strings.TrimSpace(string(cid))
	if e.ContainerID == "" {
		e.ContainerID = strings.TrimSpace(string(out))
	}
	if !dockerExactID.MatchString(e.ContainerID) {
		e.ContainerID = ""
		if err == nil {
			err = fmt.Errorf("Docker creation returned no recoverable exact container ID")
		}
		return e, err
	}
	if err != nil {
		// This read-only recovery has its own short deadline because the original
		// operation may be canceled. Never adopt by name, mutate, or roll back.
		recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, inspectErr := b.Inspect(recovery, e)
		return e, errors.Join(err, inspectErr)
	}
	if _, err := b.Inspect(ctx, e); err != nil {
		return e, err
	}
	return e, b.ready(ctx, e, user)
}
