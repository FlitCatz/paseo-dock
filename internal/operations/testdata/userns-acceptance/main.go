// userns-acceptance explicitly creates one isolated Service-owned Docker guest.
// It never authenticates Codex or contacts a model, and preserves the stopped guest.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"paseo-sbx/internal/config"
	"paseo-sbx/internal/containerpolicy"
	"paseo-sbx/internal/operationlock"
	"paseo-sbx/internal/operations"
)

const expectedImage = "sha256:7760a63ba6a62611c4434c1208c1c87236cd37d36b691da206ebd0d7e77d6521"
const namespace = "paseo-sbx-category-live-20261007"
const sourceRoot = "/Users/dev-r/devsbx/paseo-sbx/.worktrees/management-tui"

type inspection struct {
	ID    string `json:"Id"`
	Image string
	State struct {
		Running   bool
		StartedAt string
	}
	Config     struct{ Labels map[string]string }
	HostConfig struct {
		SecurityOpt, CapAdd, CapDrop []string
		Privileged                   bool
		NetworkMode                  string
		PortBindings                 map[string]json.RawMessage
	}
	Mounts []operations.RuntimeMount
}

// ownedGuest freezes the independently preserved or Service-created CID.
type ownedGuest struct {
	cid    string
	target operations.Target
}

func ownershipVerifier(ctx context.Context, inspect func(context.Context, operations.Target) error) func(operations.Target) error {
	return func(candidate operations.Target) error {
		// A canceled create can still return an exact recovered CID. Verify its
		// policy before enrollment using a separate, bounded read-only deadline.
		verification, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
		defer cancel()
		return inspect(verification, candidate)
	}
}

func (o *ownedGuest) enroll(candidate operations.Target, verify func(operations.Target) error) error {
	if o.cid != "" && candidate.Entry.ContainerID != o.cid {
		return fmt.Errorf("preserved registry/CID mismatch")
	}
	if err := verify(candidate); err != nil {
		return err
	}
	if o.cid == "" {
		o.cid = candidate.Entry.ContainerID
	}
	o.target = candidate
	return nil
}

func capture(ctx context.Context, args ...string) ([]byte, error) {
	return captureEnv(ctx, nil, args...)
}

func captureEnv(ctx context.Context, env []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	if env != nil {
		cmd.Env = env
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("%s: %w: %s", args[0], err, stderr.String())
	}
	return out, nil
}

func cleanupDaemon(ctx context.Context, home string) error {
	root := filepath.Dir(home)
	canonical, err := config.Canonical(home)
	if err != nil || canonical != home || filepath.Base(home) != "paseo" || filepath.Dir(root) != "/private/tmp" || !strings.HasPrefix(filepath.Base(root), "paseo-sbx-userns-live-") {
		return fmt.Errorf("daemon cleanup requires canonical isolated acceptance home")
	}
	pidPath := filepath.Join(home, "paseo.pid")
	var pid struct {
		PID, UID int
		ServerID string
	}
	if err := config.ReadJSON(pidPath, &pid); os.IsNotExist(err) {
		fmt.Println("ISOLATED_DAEMON_ALREADY_STOPPED")
		return nil
	} else if err != nil {
		return fmt.Errorf("daemon cleanup identity: %w", err)
	}
	if pid.PID <= 0 || pid.UID != os.Getuid() || pid.ServerID == "" {
		return fmt.Errorf("daemon cleanup identity mismatch")
	}
	env := []string{}
	for _, pair := range os.Environ() {
		key, _, _ := strings.Cut(pair, "=")
		if key != "HOME" && key != "PASEO_HOME" && key != "PASEO_HOST" {
			env = append(env, pair)
		}
	}
	env = append(env, "HOME="+filepath.Join(root, "home"), "PASEO_HOME="+home)
	out, err := captureEnv(ctx, env, "paseo", "daemon", "stop", "--home", home)
	fmt.Printf("EXACT_ISOLATED_DAEMON_STOP home=%s pid=%d: %s", home, pid.PID, out)
	if err != nil {
		return fmt.Errorf("daemon cleanup stop: %w", err)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		return fmt.Errorf("daemon cleanup PID record still present: %v", err)
	}
	fmt.Println("FINAL_ISOLATED_DAEMON_STOPPED")
	return nil
}

func execute(s *operations.Service, ctx context.Context, request operations.Request) (operations.Result, error) {
	fmt.Printf("SERVICE_OPERATION %s\n", request.Kind)
	return s.Execute(ctx, request, nil)
}

func readTarget(workspace string) (operations.Target, error) {
	runtime, err := config.RuntimePath(workspace)
	if err != nil {
		return operations.Target{}, err
	}
	var reg config.Registry
	if err := config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg); err != nil {
		return operations.Target{}, err
	}
	entry, ok := reg.Profiles["container/codex-superpowers"]
	if !ok || len(reg.Profiles) != 1 || reg.Workspace != workspace || entry.Workspace != workspace || entry.ImageID != expectedImage || entry.SecurityPolicy != containerpolicy.ID || entry.SecurityPolicyDigest != containerpolicy.SHA256 {
		return operations.Target{}, fmt.Errorf("registry identity/policy mismatch")
	}
	return operations.Target{Category: config.CategoryContainer, Workspace: workspace, Runtime: runtime, Profile: entry.Key(), Entry: entry}, nil
}

func inspect(ctx context.Context, backend *operations.DockerBackend, target operations.Target) ([]byte, error) {
	if _, err := backend.Inspect(ctx, target.Entry); err != nil {
		return nil, err
	}
	out, err := capture(ctx, "docker", "inspect", "--type", "container", target.Entry.ContainerID)
	if err != nil {
		return nil, err
	}
	var values []inspection
	if err := json.Unmarshal(out, &values); err != nil {
		return nil, err
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("inspect count")
	}
	v := values[0]
	if v.ID != target.Entry.ContainerID || v.Image != expectedImage || v.Config.Labels[operations.DockerManagedLabel] != "true" || v.Config.Labels["io.paseo-sbx.category"] != "container" || v.Config.Labels["io.paseo-sbx.security-policy"] != containerpolicy.ID || v.Config.Labels["io.paseo-sbx.security-policy-digest"] != containerpolicy.SHA256 || v.HostConfig.Privileged || len(v.HostConfig.CapAdd) != 0 || !(slices.Contains(v.HostConfig.CapDrop, "SYS_ADMIN") || slices.Contains(v.HostConfig.CapDrop, "CAP_SYS_ADMIN")) || v.HostConfig.NetworkMode != "bridge" || len(v.HostConfig.PortBindings) != 0 || len(v.HostConfig.SecurityOpt) != 2 || len(v.Mounts) != 1 || v.Mounts[0].Type != "bind" || v.Mounts[0].Source != target.Workspace || v.Mounts[0].Destination != target.Workspace || !v.Mounts[0].RW {
		return nil, fmt.Errorf("raw Docker policy/mount mismatch")
	}
	nnp, profile := false, false
	for _, opt := range v.HostConfig.SecurityOpt {
		if opt == "no-new-privileges" || opt == "no-new-privileges=true" {
			nnp = true
		}
		if strings.HasPrefix(opt, "seccomp=") && containerpolicy.MatchesJSON([]byte(strings.TrimPrefix(opt, "seccomp="))) {
			profile = true
		}
	}
	if !nnp || !profile {
		return nil, fmt.Errorf("loaded seccomp/NNP mismatch")
	}
	fmt.Printf("RAW_POLICY_STATE %s running=%t CapDrop=%v CapAdd=%v Privileged=%t NNP=%t SeccompMatches=%t Mounts=%d Ports=%d\n", v.ID, v.State.Running, v.HostConfig.CapDrop, v.HostConfig.CapAdd, v.HostConfig.Privileged, nnp, profile, len(v.Mounts), len(v.HostConfig.PortBindings))
	// Save only the inspected identity/policy fields, never guest environment.
	return json.MarshalIndent(v, "", "  ")
}

func guest(ctx context.Context, backend *operations.DockerBackend, target operations.Target, args ...string) error {
	release, err := operationlock.Acquire(ctx, target.Runtime, []string{operationlock.EntryKey(target.Entry)})
	if err != nil {
		return err
	}
	defer release()
	fresh, err := readTarget(target.Workspace)
	if err != nil {
		return err
	}
	if !fresh.EqualIdentity(target) {
		return fmt.Errorf("target changed before exec")
	}
	fmt.Printf("GUEST_PROBE %q\n", args)
	return backend.Exec(ctx, target.Entry, args, nil, os.Stdout, os.Stderr)
}

func reject(s *operations.Service, ctx context.Context, backend *operations.DockerBackend, target operations.Target, request operations.Request, label string) error {
	registryPath := filepath.Join(target.Runtime, "registry.json")
	beforeRegistry, err := os.ReadFile(registryPath)
	if err != nil {
		return err
	}
	beforeState, err := inspect(ctx, backend, target)
	if err != nil {
		return err
	}
	result, rejected := execute(s, ctx, request)
	if rejected == nil || result.Changed || result.NeedsVerification {
		return fmt.Errorf("%s was not rejected before mutation", label)
	}
	afterRegistry, err := os.ReadFile(registryPath)
	if err != nil {
		return err
	}
	afterState, err := inspect(ctx, backend, target)
	if err != nil {
		return err
	}
	if !bytes.Equal(beforeRegistry, afterRegistry) || !bytes.Equal(beforeState, afterState) {
		return fmt.Errorf("%s mutated target", label)
	}
	fmt.Printf("EXPECTED_OPERATION_REJECTION %s %v; REGISTRY_AND_TARGET_UNCHANGED\n", label, rejected)
	return nil
}

func run() (retErr error) {
	resumeRoot := flag.String("resume-root", "", "continue only this helper's preserved new acceptance guest")
	stopOnly := flag.Bool("stop-only", false, "verify and stop only the preserved acceptance guest")
	daemonCleanupOnly := flag.Bool("daemon-cleanup-only", false, "exercise only isolated provider daemon cleanup")
	flag.Parse()
	if flag.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if *stopOnly && *resumeRoot == "" {
		return fmt.Errorf("stop-only requires preserved acceptance root")
	}
	if *daemonCleanupOnly {
		if *resumeRoot == "" || *stopOnly {
			return fmt.Errorf("daemon cleanup requires only an explicit preserved root")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return cleanupDaemon(ctx, filepath.Join(*resumeRoot, "paseo"))
	}
	_, hostSet := os.LookupEnv("PASEO_HOST")
	fmt.Printf("INITIAL_PASEO_HOST_SET %t (value omitted)\n", hostSet)
	// Read only the existing daemon endpoint before replacing HOME/config.
	preflight, cancelPreflight := context.WithTimeout(context.Background(), 15*time.Second)
	endpoint := os.Getenv("DOCKER_HOST")
	if endpoint == "" {
		out, err := capture(preflight, "docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}")
		if err != nil {
			cancelPreflight()
			return err
		}
		endpoint = strings.TrimSpace(string(out))
	}
	cancelPreflight()
	if !strings.HasPrefix(endpoint, "unix:///") {
		return fmt.Errorf("acceptance requires local Unix Docker endpoint")
	}
	root := *resumeRoot
	var err error
	var expectedCID string
	if root == "" {
		root, err = os.MkdirTemp("/private/tmp", "paseo-sbx-userns-live-")
		if err != nil {
			return err
		}
	} else {
		canonical, err := config.Canonical(root)
		if err != nil || canonical != root || filepath.Dir(root) != "/private/tmp" || !strings.HasPrefix(filepath.Base(root), "paseo-sbx-userns-live-") {
			return fmt.Errorf("invalid preserved acceptance root")
		}
		// No registry is reconstructed: require the CID record and original trusted recipe.
		cid, err := os.ReadFile(filepath.Join(root, "container-id"))
		if err != nil {
			return err
		}
		expectedCID = strings.TrimSpace(string(cid))
		if len(expectedCID) != 64 {
			return fmt.Errorf("preserved exact CID missing")
		}
		for _, name := range []string{"versions.lock.json", "Dockerfile"} {
			prior, err := os.ReadFile(filepath.Join(root, "recipe", name))
			if err != nil {
				return err
			}
			current, err := os.ReadFile(filepath.Join(sourceRoot, name))
			if err != nil || !bytes.Equal(prior, current) {
				return fmt.Errorf("preserved trusted %s changed", name)
			}
		}
		fmt.Printf("CONTINUING_EXACT_ACCEPTANCE_GUEST %s\n", expectedCID)
	}
	// Process output is mirrored by main's pipe so both Service and probe output persist.
	fmt.Printf("ACCEPTANCE_ROOT %s\n", root)
	provenance, provenanceCancel := context.WithTimeout(context.Background(), 5*time.Second)
	revision, err := capture(provenance, "git", "-C", sourceRoot, "rev-parse", "HEAD")
	provenanceCancel()
	if err != nil {
		return err
	}
	fmt.Printf("CURRENT_SOURCE_REVISION %s", revision)
	workspace := filepath.Join(root, "workspace")
	for _, dir := range []string{workspace, filepath.Join(root, "home"), filepath.Join(root, "paseo"), filepath.Join(root, "docker-config"), filepath.Join(root, "recipe", "bin")} {
		canonical, err := config.Canonical(dir)
		if err != nil || canonical != dir {
			return fmt.Errorf("isolated directory must not traverse symlinks: %s", dir)
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	// Preserve trusted recipe/build inputs outside the sole mounted workspace.
	for _, name := range []string{"versions.lock.json", "catalog.json", "Dockerfile", "bin/agent-wrapper", "bin/paseo-sbx", "bin/install-runtime"} {
		destination := filepath.Join(root, "recipe", name)
		canonical, err := config.Canonical(destination)
		if err != nil || canonical != destination {
			return fmt.Errorf("trusted input destination traverses symlink")
		}
		data, err := os.ReadFile(filepath.Join(sourceRoot, name))
		if os.IsNotExist(err) && name == "catalog.json" {
			continue
		}
		if err != nil {
			return err
		}
		mode := os.FileMode(0600)
		if strings.HasPrefix(name, "bin/") {
			mode = 0700
		}
		if err := os.WriteFile(destination, data, mode); err != nil {
			return err
		}
		fmt.Printf("TRUSTED_INPUT %s SHA256=%x\n", name, sha256.Sum256(data))
	}
	// Build the existing Go ELF probe before changing the Go module/cache environment.
	probeDestination := filepath.Join(workspace, "namespace-probe")
	canonicalProbe, err := config.Canonical(probeDestination)
	if err != nil || canonicalProbe != probeDestination {
		return fmt.Errorf("probe destination traverses symlink")
	}
	build := exec.Command("go", "build", "-trimpath", "-o", probeDestination, "./internal/operations/testdata/namespace-probe")
	build.Dir = sourceRoot
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0", "GOCACHE=/private/tmp/paseo-sbx-go-cache", "GOTMPDIR=/private/tmp")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		return err
	}
	for _, key := range []string{"DOCKER_CONTEXT", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH", "PASEO_HOST", "OPENAI_API_KEY", "CODEX_API_KEY", "CHATGPT_API_KEY", "ANTHROPIC_API_KEY"} {
		if err := os.Unsetenv(key); err != nil {
			return err
		}
	}
	for key, value := range map[string]string{"HOME": filepath.Join(root, "home"), "PASEO_HOME": filepath.Join(root, "paseo"), "DOCKER_CONFIG": filepath.Join(root, "docker-config"), "DOCKER_HOST": endpoint, "PASEO_SBX_AUTO_UPDATE": "0"} {
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	fmt.Printf("ISOLATED_HOME %s\nISOLATED_PASEO_HOME %s\nDOCKER_ENDPOINT %s\nPOLICY %s SHA256=%s\n", os.Getenv("HOME"), os.Getenv("PASEO_HOME"), endpoint, containerpolicy.ID, containerpolicy.SHA256)
	// Normal Service routes and provider outputs remain inside the trusted copy.
	s, err := operations.New(filepath.Join(root, "recipe"))
	if err != nil {
		return err
	}
	s.In = nil
	selection := config.Selection{Schema: 2, Category: config.CategoryContainer, CodexUserNS: true, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}, Utilities: []string{"rtk"}}
	selection, err = s.Catalog.Normalize(selection)
	if err != nil {
		return err
	}
	imageEntry := config.Entry{Category: selection.Category, Agent: "codex", Tool: "superpowers", Utilities: selection.Utilities}
	tag, _, err := operations.ImageSpec(selection.Category, imageEntry, s.Lock)
	if err != nil {
		return err
	}
	ref := namespace + "/" + imageEntry.Key() + ":" + tag
	ctx, cancelSignal := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancelSignal()
	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	out, err := capture(ctx, "docker", "image", "inspect", "--format", "{{.Id}} {{.Architecture}}", ref)
	if err != nil {
		return fmt.Errorf("normal computed image reference unavailable; no build/tag attempted: %w", err)
	}
	if strings.TrimSpace(string(out)) != expectedImage+" arm64" {
		return fmt.Errorf("normal computed image identity differs: %s", out)
	}
	fmt.Printf("NORMAL_IMAGE_REF %s\nEXACT_IMAGE %s", ref, out)
	backend := &operations.DockerBackend{Root: s.Root}
	owner := ownedGuest{cid: expectedCID}
	verify := ownershipVerifier(ctx, func(verification context.Context, candidate operations.Target) error {
		_, err := inspect(verification, backend, candidate)
		return err
	})
	defer func() {
		if owner.target.Entry.ContainerID == "" {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		_, err := execute(s, cleanup, operations.Request{Kind: operations.KindStop, Target: owner.target})
		if err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("cleanup stop failed: %w", err))
			return
		}
		state, err := backend.Inspect(cleanup, owner.target.Entry)
		if err != nil || state.Running {
			retErr = errors.Join(retErr, fmt.Errorf("cleanup running=false verification failed: %v", err))
			return
		}
		fmt.Printf("FINAL_RUNNING_FALSE %s\n", owner.target.Entry.ContainerID)
	}()
	if expectedCID != "" {
		candidate, err := readTarget(workspace)
		if err != nil {
			return err
		}
		if err := owner.enroll(candidate, verify); err != nil {
			return err
		}
	}
	if *stopOnly {
		fmt.Println("CLEANUP_ONLY_REQUEST; no creation, exec, install or acceptance claim")
		return nil
	}
	target := owner.target
	request := operations.Request{Kind: operations.KindCreate, Target: operations.Target{Workspace: workspace}, Selection: selection, Namespace: namespace}
	if expectedCID != "" {
		request.Target = target
	}
	result, err := execute(s, ctx, request)
	// Recover only the exact Service-returned target on partial creation failures.
	if len(result.Remaining) == 1 {
		candidate := result.Remaining[0]
		fmt.Printf("SERVICE_RETURNED_CONTAINER %s\n", candidate.Entry.ContainerID)
		if enrollmentErr := owner.enroll(candidate, verify); enrollmentErr != nil {
			return errors.Join(err, enrollmentErr)
		}
		target = owner.target
	}
	if target.Entry.ContainerID != "" {
		fmt.Printf("EXACT_CONTAINER %s\n", target.Entry.ContainerID)
		if e := os.WriteFile(filepath.Join(root, "container-id"), []byte(target.Entry.ContainerID+"\n"), 0600); e != nil {
			return errors.Join(err, e)
		}
	}
	if err != nil {
		return err
	}
	if owner.target.Entry.ContainerID == "" {
		return fmt.Errorf("Service returned no verified owned container identity")
	}
	confirmed, err := readTarget(workspace)
	if err != nil {
		return err
	}
	if err := owner.enroll(confirmed, verify); err != nil {
		return err
	}
	target = owner.target
	var reg config.Registry
	if err := config.ReadJSON(filepath.Join(target.Runtime, "registry.json"), &reg); err != nil {
		return err
	}
	var saved config.Selection
	if err := config.ReadJSON(filepath.Join(target.Runtime, "selection.json"), &saved); err != nil {
		return err
	}
	if reg.Selection == nil || !reg.Selection.CodexUserNS || !saved.CodexUserNS || saved.Category != config.CategoryContainer || len(reg.Active) != 1 || reg.Active[0] != target.Profile {
		return fmt.Errorf("saved selection/activation mismatch")
	}
	metadata, _ := json.MarshalIndent(reg, "", "  ")
	fmt.Printf("SAVED_REGISTRY %s\n", metadata)
	policy, err := inspect(ctx, backend, target)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "docker-policy.json"), policy, 0600); err != nil {
		return err
	}
	fmt.Printf("RAW_DOCKER_POLICY_VERIFIED SHA256=%x; no privileged/add-capabilities; drop SYS_ADMIN; sole workspace bind\n", sha256.Sum256(policy))
	// Registration may start a new local daemon. Its private home is uniquely
	// owned by this run; never stop through a default/shared daemon endpoint.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		retErr = errors.Join(retErr, cleanupDaemon(cleanup, filepath.Join(root, "paseo")))
	}()
	if _, err := execute(s, ctx, operations.Request{Kind: operations.KindRegister, Target: target, Targets: []operations.Target{target}, PaseoHome: os.Getenv("PASEO_HOME"), Apply: true}); err != nil {
		return err
	}
	base, err := config.BaseDir()
	if err != nil {
		return err
	}
	launcher, err := os.ReadFile(config.LauncherPath(base))
	if err != nil {
		return err
	}
	trusted, err := os.ReadFile(filepath.Join(sourceRoot, "bin", "agent-wrapper"))
	if err != nil || !bytes.Equal(launcher, trusted) {
		return fmt.Errorf("registered launcher differs from latest host build")
	}
	fmt.Println("REGISTERED_LATEST_LAUNCHER_ISOLATED_PROVIDER")
	probe := filepath.Join(workspace, "namespace-probe")
	if err := guest(ctx, backend, target, "sh", "-c", `test ! -e /home/agent/.codex/auth.json && test -z "${OPENAI_API_KEY-}" && test -z "${CODEX_API_KEY-}" && printf 'guest-home-persistence\n' > /home/agent/userns-persistence`); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(workspace, "userns-persistence"), []byte("workspace-persistence\n"), 0600); err != nil {
		return err
	}
	for _, args := range [][]string{{"id"}, {"bwrap", "--unshare-user", "--ro-bind", "/", "/", "--", "true"}, {"sh", "-c", `test "$(codex --version)" = 'codex-cli 0.159.2'; codex --version`}, {probe, "prepare", workspace}, {"codex", "sandbox", "-c", `sandbox_mode="workspace-write"`, "--", probe, "sandbox", workspace}, {"env", "-C", "/tmp", "npm", "install", "--global", "--prefix", "/home/agent/.local", "@openai/codex@0.161.0"}, {"sh", "-c", `test "$(/home/agent/.local/bin/codex --version)" = 'codex-cli 0.161.0'; /home/agent/.local/bin/codex --version`}, {probe, "prepare", workspace}, {"/home/agent/.local/bin/codex", "sandbox", "-c", `sandbox_mode="workspace-write"`, "--", probe, "sandbox", workspace}} {
		if err := guest(ctx, backend, target, args...); err != nil {
			return err
		}
	}
	for _, field := range []string{"policy", "digest"} {
		forged := target
		if field == "policy" {
			forged.Entry.SecurityPolicy = "forged-policy"
		} else {
			forged.Entry.SecurityPolicyDigest = strings.Repeat("0", 64)
		}
		if err := reject(s, ctx, backend, target, operations.Request{Kind: operations.KindStop, Target: forged}, "forged-"+field); err != nil {
			return err
		}
	}
	otherSelection := selection
	otherSelection.CodexUserNS = false
	if err := reject(s, ctx, backend, target, operations.Request{Kind: operations.KindCreate, Target: target, Selection: otherSelection, Namespace: namespace}, "existing-policy-change"); err != nil {
		return err
	}
	if _, err := execute(s, ctx, operations.Request{Kind: operations.KindStop, Target: target}); err != nil {
		return err
	}
	state, err := backend.Inspect(ctx, target.Entry)
	if err != nil || state.Running {
		return fmt.Errorf("intermediate stop state mismatch: %v", err)
	}
	request.Target = target
	if _, err := execute(s, ctx, request); err != nil {
		return err
	}
	resumed, err := readTarget(workspace)
	if err != nil || !resumed.EqualIdentity(target) {
		return fmt.Errorf("Service resume changed exact identity: %v", err)
	}
	if _, err := inspect(ctx, backend, target); err != nil {
		return err
	}
	if err := guest(ctx, backend, target, "sh", "-c", `test "$(cat /home/agent/userns-persistence)" = guest-home-persistence && test "$(cat userns-persistence)" = workspace-persistence && test ! -e /home/agent/.codex/auth.json`); err != nil {
		return err
	}
	if err := guest(ctx, backend, target, "/home/agent/.local/bin/codex", "sandbox", "-c", `sandbox_mode="workspace-write"`, "--", probe, "sandbox", workspace); err != nil {
		return err
	}
	fmt.Printf("SERVICE_RESUME_EXACT_ID_POLICY_HOME_WORKSPACE_PERSISTENCE_PASSED %s\nNO_LOGIN_OR_MODEL_REQUEST\n", target.Entry.ContainerID)
	return nil
}

func main() {
	// Tee stdout/stderr into an externally supplied durable log, if requested.
	var writer io.Writer = os.Stdout
	if path := os.Getenv("PASEO_SBX_ACCEPTANCE_LOG"); path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer f.Close()
		writer = io.MultiWriter(os.Stdout, f)
	}
	reader, pipe, err := os.Pipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Stdout, os.Stderr = pipe, pipe
	done := make(chan error, 1)
	go func() { _, err := io.Copy(writer, reader); done <- err }()
	err = run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ACCEPTANCE_FAILED", err)
	} else {
		fmt.Println("HELPER_PASSED")
	}
	pipe.Close()
	copyErr := <-done
	if err != nil || copyErr != nil {
		os.Exit(1)
	}
}
