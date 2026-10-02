package operations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operationlock"
	"paseo-sbx/internal/sandboxupdate"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Prune must neither overlap a live installer nor unlink the inode held by its lock.
func TestPruneDuringUpdatePreservesSandboxLock(t *testing.T) {
	s, target, _ := operationFixture(t)
	if err := os.MkdirAll(filepath.Join(s.Root, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Root, "bin", "agent-wrapper"), []byte("trusted test launcher"), 0700); err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	if err := os.Symlink(exe, filepath.Join(filepath.Dir(os.Getenv("SBX_TEST_LOG")), "paseo")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- sandboxupdate.UpdateContext(ctx, target.Runtime, target.Entry, true, io.Discard, nil) }()
	waitReady(t)
	if err := os.RemoveAll(target.Workspace); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(target.Entry.Sandbox))
	lockPath := filepath.Join(target.Runtime, "updates", fmt.Sprintf("%x.lock", sum[:8]))
	before, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Execute(context.Background(), Request{Kind: KindPrune}, nil)
	if !errors.Is(err, operationlock.ErrConflict) {
		t.Errorf("prune overlapped active installer: %v", err)
	}
	after, statErr := os.Stat(lockPath)
	if statErr != nil || !os.SameFile(before, after) {
		t.Error("prune replaced the live lock inode", statErr)
	}
	release, err := operationlock.Acquire(context.Background(), target.Runtime, []string{operationlock.SandboxKey(target.Entry.Sandbox)})
	if err == nil {
		release()
		t.Error("prune allowed reacquisition while installer holds lock")
	}
	if _, err := os.Stat(filepath.Join(target.Runtime, "registry.json")); err != nil {
		t.Error("conflicting prune forgot registry", err)
	}
	cancel()
	<-done
	// Completed pruning still retains every historical lock inode, including an
	// unregistered sandbox's lock file, so stale automatic callers cannot bypass it.
	orphanPath := filepath.Join(target.Runtime, "updates", "historical.lock")
	if err := os.WriteFile(orphanPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	orphanBefore, err := os.Stat(orphanPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Execute(context.Background(), Request{Kind: KindPrune}, nil); err != nil {
		t.Fatal("prune failed after installer released", err)
	}
	after, err = os.Stat(lockPath)
	if err != nil || !os.SameFile(before, after) {
		t.Error("completed prune replaced sandbox lock inode", err)
	}
	orphanAfter, err := os.Stat(orphanPath)
	if err != nil || !os.SameFile(orphanBefore, orphanAfter) {
		t.Error("prune removed historical lock inode", err)
	}
	if _, err := os.Stat(filepath.Join(target.Runtime, "registry.json")); !os.IsNotExist(err) {
		t.Error("prune did not forget registry", err)
	}
	release, err = operationlock.Acquire(context.Background(), target.Runtime, []string{operationlock.SandboxKey(target.Entry.Sandbox)})
	if err != nil {
		t.Fatal("released lock blocked retry after prune", err)
	}
	release()
}

// External sbx is a Go test executable; registry, updater and process boundaries stay real.
func TestMain(m *testing.M) {
	if os.Getenv("OBSERVE_TEST_MODE") == "1" && slices.Contains([]string{"sbx", "paseo", "docker"}, filepath.Base(os.Args[0])) {
		observationFake()
	}
	if filepath.Base(os.Args[0]) == "paseo" {
		if slices.Contains(os.Args, "status") {
			fmt.Println(`{"localDaemon":"running"}`)
		}
		os.Exit(0)
	}
	if filepath.Base(os.Args[0]) == "docker" {
		fmt.Println("build stdout")
		fmt.Fprintln(os.Stderr, "build stderr")
		os.Exit(0)
	}
	if filepath.Base(os.Args[0]) == "sbx" {
		fakeSBX()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// A shared service must send job output only to that job's observer.
func TestExecuteLogsAreRequestScoped(t *testing.T) {
	s, _, _ := operationFixture(t)
	loaded, err := New("../..")
	if err != nil {
		t.Fatal(err)
	}
	s.Lock = loaded.Lock
	s.Catalog = loaded.Catalog
	exe, _ := os.Executable()
	if err = os.Symlink(exe, filepath.Join(filepath.Dir(os.Getenv("SBX_TEST_LOG")), "docker")); err != nil {
		t.Fatal(err)
	}
	var terminal bytes.Buffer
	s.Out = &terminal
	s.Err = &terminal
	selection := config.Selection{Schema: 1, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}}
	var events []Event
	result, err := s.Execute(context.Background(), Request{Kind: KindBuild, Selection: selection}, func(e Event) { events = append(events, e) })
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("successful build not reported changed")
	}
	var logs string
	for _, e := range events {
		if e.Time.IsZero() {
			t.Fatal("event has no timestamp")
		}
		logs += e.Text
	}
	if !strings.Contains(logs, "build stdout") || !strings.Contains(logs, "build stderr") {
		t.Errorf("subprocess logs missing from request events: %s", logs)
	}
	if terminal.Len() != 0 {
		t.Errorf("background request wrote terminal streams: %s", terminal.String())
	}
	if s.Out != &terminal || s.Err != &terminal {
		t.Fatal("shared service streams mutated")
	}
}

// Invalid selection schemas must fail before shared artifacts or subprocesses change.
func TestExecuteRejectsInvalidSelection(t *testing.T) {
	s, _, _ := operationFixture(t)
	loaded, err := New("../..")
	if err != nil {
		t.Fatal(err)
	}
	s.Lock = loaded.Lock
	s.Catalog = loaded.Catalog
	selection := config.Selection{Schema: 2, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}}
	_, err = s.Execute(context.Background(), Request{Kind: KindBuild, Selection: selection}, nil)
	if err == nil {
		t.Error("invalid selection schema accepted")
	}
	if _, err = os.Stat(filepath.Join(s.Root, ".generated", "bake.json")); !os.IsNotExist(err) {
		t.Error("invalid selection wrote build artifact")
	}
}
func fakeSBX() {
	if os.Getenv("SBX_TEST_STDIN") == "1" {
		b, _ := io.ReadAll(os.Stdin)
		fmt.Printf("stdin:%s", b)
		return
	}
	args := os.Args[1:]
	command := strings.Join(args, " ")
	f, _ := os.OpenFile(os.Getenv("SBX_TEST_LOG"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if f != nil {
		fmt.Fprintln(f, command)
		f.Close()
	}
	if len(args) == 0 || args[0] != "exec" {
		if len(args) > 0 && args[0] == "ls" {
			fmt.Println("[]")
		}
		if len(args) > 0 && args[0] == "create" && os.Getenv("SBX_TEST_CREATE_FAIL") != "" && strings.Contains(command, os.Getenv("SBX_TEST_CREATE_FAIL")) {
			os.Exit(1)
		}
		if len(args) > 0 && args[0] == "stop" && os.Getenv("SBX_TEST_STOP_BLOCK") == "1" {
			_ = os.WriteFile(os.Getenv("SBX_TEST_READY"), nil, 0600)
			time.Sleep(2 * time.Second)
		}
		return
	}
	args = args[4:]
	command = strings.Join(args, " ")
	switch {
	case command == "printenv HOME":
		fmt.Println("/home/agent")
	case strings.HasPrefix(command, "npm view"):
		fmt.Println("9.0.0")
	case command == "codex --version":
		fmt.Println("1.0.0")
	case strings.HasPrefix(command, "mktemp -d"):
		fmt.Println("/home/agent/.local/share/paseo-sbx/updates/install.test")
	case strings.HasPrefix(command, "npm install"):
		_ = os.WriteFile(os.Getenv("SBX_TEST_READY"), []byte("ready"), 0600)
		end := time.Now().Add(2 * time.Second)
		for time.Now().Before(end) {
			if _, err := os.Stat(os.Getenv("SBX_TEST_RELEASE")); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		os.Exit(1)
	case strings.HasPrefix(command, "cat "):
		os.Exit(1)
	case strings.HasPrefix(command, "mktemp -u"):
		fmt.Println(strings.TrimSuffix(args[2], "XXXXXXXXXX") + "test")
	case strings.HasSuffix(command, "--version"):
		fmt.Println("9.0.0")
	}
}

func TestExecuteBackgroundDoesNotConsumeTerminalInput(t *testing.T) {
	s, target, _ := operationFixture(t)
	t.Setenv("SBX_TEST_STDIN", "1")
	input := bytes.NewBufferString("terminal keys")
	s.In = input
	var events strings.Builder
	_, err := s.Execute(context.Background(), Request{Kind: KindStop, Target: target}, func(e Event) { events.WriteString(e.Text) })
	if err != nil {
		t.Fatal(err)
	}
	if input.String() != "terminal keys" || strings.Contains(events.String(), "stdin:terminal keys") {
		t.Fatalf("background subprocess consumed terminal input: remaining %q events %q", input.String(), events.String())
	}
	var out bytes.Buffer
	s.Out = &out
	_, err = s.Execute(context.Background(), Request{Kind: KindStop, Target: target}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "stdin:terminal keys" || input.Len() != 0 || s.In != input {
		t.Fatalf("CLI stdin changed: %q / %q", out.String(), input.String())
	}
}

func TestExecuteReportsPartialCreation(t *testing.T) {
	s, target, _ := operationFixture(t)
	loaded, err := New("../..")
	if err != nil {
		t.Fatal(err)
	}
	s.Lock = loaded.Lock
	s.Catalog = loaded.Catalog
	if err := os.RemoveAll(target.Runtime); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SBX_TEST_CREATE_FAIL", "codex-superpowers")
	result, err := s.Execute(context.Background(), Request{Kind: KindCreate, Target: Target{Workspace: target.Workspace}, Selection: config.Selection{Schema: 1, Pairs: []config.Pair{{Agent: "claude", Methodology: "superpowers"}, {Agent: "codex", Methodology: "superpowers"}}}}, nil)
	if err == nil {
		t.Fatal("partial creation succeeded unexpectedly")
	}
	if !result.Changed || len(result.Remaining) != 1 || result.Remaining[0].Profile != "claude-superpowers" {
		t.Fatalf("partial creation lost successful resource: %+v", result)
	}
	var reg config.Registry
	if err := config.ReadJSON(filepath.Join(target.Runtime, "registry.json"), &reg); err != nil {
		t.Fatal(err)
	}
	if len(reg.ActiveProfiles()) != 0 || reg.Selection != nil {
		t.Fatal("partial creation activated incomplete selection", reg)
	}
}

func TestExecuteFreezesTargetsBeforeEvents(t *testing.T) {
	s, target, log := operationFixture(t)
	other := target
	other.Profile = "other"
	other.Entry.Sandbox = "two"
	var reg config.Registry
	_ = config.ReadJSON(filepath.Join(target.Runtime, "registry.json"), &reg)
	reg.Profiles[other.Profile] = other.Entry
	if err := config.WriteJSON(filepath.Join(target.Runtime, "registry.json"), reg); err != nil {
		t.Fatal(err)
	}
	request := Request{Kind: KindStop, Targets: []Target{target}}
	_, err := s.Execute(context.Background(), request, func(Event) { request.Targets[0] = other })
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	if string(b) != "stop one\n" {
		t.Fatalf("event callback changed confirmed request: %s", b)
	}
}

func TestCanceledUpdateRecordFailureStillNeedsVerification(t *testing.T) {
	s, target, _ := operationFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() { r, e := s.Execute(ctx, Request{Kind: KindUpdate, Target: target}, nil); done <- outcome{r, e} }()
	waitReady(t)
	sum := sha256.Sum256([]byte(target.Entry.Sandbox))
	if err := os.Mkdir(filepath.Join(target.Runtime, "updates", fmt.Sprintf("%x.json", sum[:8])), 0700); err != nil {
		t.Fatal(err)
	}
	cancel()
	got := <-done
	if got.err == nil || !got.result.NeedsVerification {
		t.Fatalf("state-write failure hid uncertain cancellation: %+v %v", got.result, got.err)
	}
}

func TestStopCancellationNeedsVerification(t *testing.T) {
	s, target, _ := operationFixture(t)
	t.Setenv("SBX_TEST_STOP_BLOCK", "1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() { r, e := s.Execute(ctx, Request{Kind: KindStop, Target: target}, nil); done <- outcome{r, e} }()
	waitReady(t)
	cancel()
	got := <-done
	if !errors.Is(got.err, context.Canceled) || !got.result.NeedsVerification {
		t.Fatalf("interrupted stop lost uncertain result: %+v %v", got.result, got.err)
	}
}
func operationFixture(t *testing.T) (*Service, Target, string) {
	t.Helper()
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(exe, filepath.Join(dir, "sbx")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	log := filepath.Join(dir, "commands")
	t.Setenv("SBX_TEST_LOG", log)
	t.Setenv("SBX_TEST_READY", filepath.Join(dir, "ready"))
	t.Setenv("SBX_TEST_RELEASE", filepath.Join(dir, "release"))
	w, err := config.Canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := config.RuntimePath(w)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err = config.Canonical(runtime)
	if err != nil {
		t.Fatal(err)
	}
	target := Target{Workspace: w, Runtime: runtime, Profile: "codex-native", Entry: config.Entry{Workspace: w, Sandbox: "one", Agent: "codex", Tool: "native", Image: "image"}}
	writeTarget(t, target)
	return &Service{Root: t.TempDir(), Out: io.Discard, Err: io.Discard}, target, log
}

// A prune preview exposes the exact missing registry identities without removing them.
func TestPruneReturnsMissingTargets(t *testing.T) {
	s, target, _ := operationFixture(t)
	if err := os.RemoveAll(target.Workspace); err != nil {
		t.Fatal(err)
	}
	result, err := s.Execute(context.Background(), Request{Kind: KindPrune, DryRun: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Missing) != 1 || result.Missing[0] != target {
		t.Errorf("prune preview lost missing target: %+v", result)
	}
	if result.Changed {
		t.Fatal("prune preview reported a mutation")
	}
	if _, err := os.Stat(filepath.Join(target.Runtime, "registry.json")); err != nil {
		t.Fatal("prune preview removed registry", err)
	}
}

func TestUpdateContextHonorsExternalCancellation(t *testing.T) {
	_, target, log := operationFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var stages []string
	go func() {
		done <- sandboxupdate.UpdateContext(ctx, target.Runtime, target.Entry, true, io.Discard, func(stage string) { stages = append(stages, stage) })
	}()
	waitReady(t)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("caller cancellation lost", err)
		}
	case <-time.After(time.Second):
		t.Fatal("external context did not stop installer")
	}
	if !slices.Contains(stages, "environment") || !slices.Contains(stages, "agent") {
		t.Fatal("installer stages missing", stages)
	}
	b, _ := os.ReadFile(log)
	if strings.Contains(string(b), "stop ") {
		t.Fatal("updater canceled by stopping sandbox")
	}
}

func TestShellCommandDoesNotUpdate(t *testing.T) {
	s, target, log := operationFixture(t)
	cmd, err := s.ShellCommand(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Run(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	want := "exec -it --workdir " + target.Workspace + " one bash\n"
	if string(b) != want {
		t.Fatalf("shell performed hidden operations: %s", b)
	}
}
func writeTarget(t *testing.T, target Target) {
	t.Helper()
	if err := config.WriteJSON(filepath.Join(target.Runtime, "registry.json"), config.Registry{Workspace: target.Workspace, Active: []string{target.Profile}, Profiles: map[string]config.Entry{target.Profile: target.Entry}}); err != nil {
		t.Fatal(err)
	}
}
func waitReady(t *testing.T) {
	t.Helper()
	end := time.Now().Add(3 * time.Second)
	for time.Now().Before(end) {
		if _, err := os.Stat(os.Getenv("SBX_TEST_READY")); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("installer never started")
}

// Re-resolving a confirmed target would operate on an unconfirmed replacement.
func TestExecuteRejectsChangedTarget(t *testing.T) {
	for _, kind := range []Kind{KindStop, KindUpdate, KindRegister, KindUnregister} {
		t.Run(string(kind), func(t *testing.T) {
			s, target, log := operationFixture(t)
			if err := os.MkdirAll(filepath.Join(s.Root, "bin"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(s.Root, "bin", "agent-wrapper"), []byte("trusted test launcher"), 0700); err != nil {
				t.Fatal(err)
			}
			exe, _ := os.Executable()
			if err := os.Symlink(exe, filepath.Join(filepath.Dir(log), "paseo")); err != nil {
				t.Fatal(err)
			}
			replacement := target
			replacement.Entry.Sandbox = "two"
			writeTarget(t, replacement)
			_, err := s.Execute(context.Background(), Request{Kind: kind, Target: target}, nil)
			if err == nil {
				t.Error("changed confirmed target accepted")
			}
			b, _ := os.ReadFile(log)
			if len(b) > 0 {
				t.Errorf("executed unconfirmed target: %s", b)
			}
			var reg config.Registry
			if err := config.ReadJSON(filepath.Join(target.Runtime, "registry.json"), &reg); err != nil {
				t.Fatal(err)
			}
			if len(reg.ActiveProfiles()) != 1 {
				t.Error("changed target modified registry activation")
			}
			if _, err := os.Stat(filepath.Join(s.Root, ".generated", "paseo.providers.json")); !os.IsNotExist(err) {
				t.Error("changed target wrote provider configuration")
			}
		})
	}
}

// Cancellation during a guest installer must preserve partial state and require verification.
func TestUpdateContextCancellation(t *testing.T) {
	s, target, log := operationFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() { r, e := s.Execute(ctx, Request{Kind: KindUpdate, Target: target}, nil); done <- outcome{r, e} }()
	waitReady(t)
	cancel()
	select {
	case got := <-done:
		if got.err == nil || !got.result.NeedsVerification {
			t.Errorf("cancellation lost verification requirement: %+v %v", got.result, got.err)
		}
	case <-time.After(time.Second):
		t.Error("installer ignored caller cancellation")
		<-done
	}
	sum := sha256.Sum256([]byte("one"))
	var state sandboxupdate.State
	if err := config.ReadJSON(filepath.Join(target.Runtime, "updates", fmt.Sprintf("%x.json", sum[:8])), &state); err != nil {
		t.Fatal(err)
	}
	if state.Attempt.IsZero() || len(state.Errors) == 0 || !state.Success.IsZero() {
		t.Fatalf("partial state lost: %+v", state)
	}
	b, _ := os.ReadFile(log)
	if strings.Contains(string(b), "stop ") {
		t.Fatal("cancellation stopped sandbox", string(b))
	}
}

// Explicit update and stop share the automatic updater's lock, without blocking another sandbox.
func TestSharedUpdateLock(t *testing.T) {
	s, target, _ := operationFixture(t)
	done := make(chan error, 1)
	go func() { done <- sandboxupdate.Update(target.Runtime, target.Entry, true, io.Discard) }()
	waitReady(t)
	for _, kind := range []Kind{KindUpdate, KindStop} {
		if _, err := s.Execute(context.Background(), Request{Kind: kind, Target: target}, nil); err == nil {
			t.Errorf("%s overlapped installer", kind)
		}
	}
	if err := sandboxupdate.Update(target.Runtime, target.Entry, false, io.Discard); err != nil {
		t.Fatal("automatic launch should skip conflicting updater", err)
	}
	other := target
	other.Profile = "claude-native"
	other.Entry.Sandbox = "unrelated"
	var reg config.Registry
	_ = config.ReadJSON(filepath.Join(target.Runtime, "registry.json"), &reg)
	reg.Profiles[other.Profile] = other.Entry
	if err := config.WriteJSON(filepath.Join(target.Runtime, "registry.json"), reg); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Execute(context.Background(), Request{Kind: KindStop, Target: other}, nil); err != nil {
		t.Fatal("unrelated sandbox blocked", err)
	}
	_ = os.WriteFile(os.Getenv("SBX_TEST_RELEASE"), nil, 0600)
	<-done
	if _, err := s.Execute(context.Background(), Request{Kind: KindStop, Target: target}, nil); err != nil {
		t.Fatal("released lock blocked retry", err)
	}
}
