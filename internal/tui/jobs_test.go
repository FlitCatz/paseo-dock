package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operations"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// The backend double replaces external resources; all queue/model transitions stay real.
type jobBackend struct {
	mu          sync.Mutex
	requests    []operations.Request
	snapshot    operations.Snapshot
	execute     func(context.Context, operations.Request, func(operations.Event)) (operations.Result, error)
	shellErr    error
	shellTarget operations.Target
}

func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "sbx" && os.Getenv("TUI_JOB_PROCESS") == "1" {
		args := os.Args[1:]
		f, err := os.OpenFile(os.Getenv("TUI_JOB_COMMANDS"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			os.Exit(2)
		}
		fmt.Fprintln(f, strings.Join(args, " "))
		f.Close()
		if len(args) > 0 && args[0] == "ls" {
			fmt.Println(`[{"name":"one","status":"running"},{"name":"two","status":"stopped"}]`)
		}
		if len(args) > 1 && args[0] == "stop" && args[1] == "one" {
			os.WriteFile(os.Getenv("TUI_JOB_READY"), nil, 0600)
			end := time.Now().Add(3 * time.Second)
			for time.Now().Before(end) {
				if _, err := os.Stat(os.Getenv("TUI_JOB_RELEASE")); err == nil {
					os.Exit(0)
				}
				time.Sleep(5 * time.Millisecond)
			}
			os.Exit(3)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func (b *jobBackend) Snapshot(context.Context, operations.Query) (operations.Snapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.snapshot, nil
}
func (b *jobBackend) Execute(ctx context.Context, r operations.Request, emit func(operations.Event)) (operations.Result, error) {
	b.mu.Lock()
	b.requests = append(b.requests, r)
	b.mu.Unlock()
	if b.execute != nil {
		return b.execute(ctx, r, emit)
	}
	return operations.Result{}, nil
}
func (b *jobBackend) ShellCommand(_ context.Context, t operations.Target) (*exec.Cmd, error) {
	b.shellTarget = t
	return exec.Command("true"), b.shellErr
}
func (b *jobBackend) SavedSelection(string) (config.Selection, error) { return config.Selection{}, nil }
func managementFixture() operations.Snapshot {
	s := fixtureSnapshot()
	for i := range s.Workspaces {
		w := &s.Workspaces[i]
		w.Runtime = w.ID
		for j := range w.Sandboxes {
			r := &w.Sandboxes[j]
			r.Target.Runtime = w.ID
			r.Target.Workspace = w.Path
			r.Target.Entry.Workspace = w.Path
			r.Active = true
		}
	}
	return s
}
func keyCmd(m *Dashboard, k string) tea.Cmd {
	var msg tea.KeyPressMsg
	switch k {
	case "enter":
		msg.Code = tea.KeyEnter
	case "esc":
		msg.Code = tea.KeyEscape
	case "ctrl+c":
		msg.Code = 'c'
		msg.Mod = tea.ModCtrl
	default:
		msg.Code = []rune(k)[0]
		msg.Text = k
	}
	_, cmd := m.Update(msg)
	return cmd
}
func TestJobTargetsRemainFrozen(t *testing.T) {
	b := &jobBackend{snapshot: managementFixture()}
	m := NewDashboard(b, config.DefaultCatalog(), "")
	deliver(m, b.snapshot)
	keyCmd(m, "u")
	if view := m.View().Content; !strings.Contains(view, "sandbox-one") || !strings.Contains(view, "시작") {
		t.Fatalf("missing fixed target confirmation/start warning: %q", view)
	}
	m.ProfileID = "a-two"
	s := managementFixture()
	s.Workspaces[0].Sandboxes[0].Target.Entry.Sandbox = "replacement"
	deliver(m, s)
	cmd := keyCmd(m, "enter")
	if cmd == nil {
		t.Fatal("confirmed update did not dispatch")
	}
	runJobCommand(m, cmd)
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.requests) != 1 || b.requests[0].Target.Entry.Sandbox != "sandbox-one" {
		t.Fatalf("confirmed target changed: %+v", b.requests)
	}
}

// Run immediate command batches, leaving event wait/poll commands to the actual program.
func runJobCommand(m *Dashboard, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		responses := make(chan tea.Msg, len(batch))
		for _, c := range batch {
			if c == nil {
				continue
			}
			go func(c tea.Cmd) { responses <- c() }(c)
		}
		for {
			select {
			case msg := <-responses:
				if _, ok := msg.(jobDoneMsg); ok {
					m.Update(msg)
					return
				}
			case <-time.After(2 * time.Second):
				panic("job command timed out")
			}
		}
	}
	m.Update(msg)
}
func TestJobReadOnlyActionsDisabled(t *testing.T) {
	m := NewDashboard(&jobBackend{}, config.DefaultCatalog(), "")
	s := managementFixture()
	s.Unmanaged = []operations.Sandbox{{Target: operations.Target{Entry: config.Entry{Sandbox: "external"}}, RuntimeState: "running"}}
	deliver(m, s)
	keyCmd(m, "v")
	keyCmd(m, "enter")
	if view := m.View().Content; !strings.Contains(view, "읽기 전용") {
		t.Fatalf("unmanaged action reason absent: %q", view)
	}
}

func startJobCommands(cmd tea.Cmd) <-chan tea.Msg {
	messages := make(chan tea.Msg, 32)
	var start func(tea.Cmd)
	start = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			msg := c()
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, nested := range batch {
					start(nested)
				}
				return
			}
			messages <- msg
		}()
	}
	start(cmd)
	return messages
}
func awaitJobDone(t *testing.T, ch <-chan tea.Msg) jobDoneMsg {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case msg := <-ch:
			if d, ok := msg.(jobDoneMsg); ok {
				return d
			}
		case <-timer.C:
			t.Fatal("worker did not complete")
			return jobDoneMsg{}
		}
	}
}
func TestOneMutationAtATime(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	b := &jobBackend{snapshot: managementFixture(), execute: func(ctx context.Context, r operations.Request, emit func(operations.Event)) (operations.Result, error) {
		started <- r.Target.Entry.Sandbox
		if r.Target.Entry.Sandbox == "sandbox-one" {
			<-release
			return operations.Result{Remaining: []operations.Target{r.Target}}, errors.New("first failed")
		}
		return operations.Result{Changed: true}, nil
	}}
	m := NewDashboard(b, config.DefaultCatalog(), "")
	defer m.cancel()
	deliver(m, b.snapshot)
	first := m.enqueue(operations.Request{Kind: operations.KindStop, Target: m.rows()[0].Target})
	events := startJobCommands(first)
	<-started
	second := m.enqueue(operations.Request{Kind: operations.KindStop, Target: m.rows()[1].Target})
	if second != nil {
		t.Fatal("second mutation dispatched before first completed")
	}
	snapshot := m.refresh(time.Now(), false)
	if snapshot == nil {
		t.Fatal("mutation blocked read-only snapshot")
	}
	m.Update(snapshot())
	select {
	case <-started:
		t.Fatal("queued mutation overlapped first")
	default:
	}
	close(release)
	_, next := m.Update(awaitJobDone(t, events))
	follow := startJobCommands(next)
	if sandbox := <-started; sandbox != "sandbox-two" {
		t.Fatal("wrong next target", sandbox)
	}
	m.Update(awaitJobDone(t, follow))
	if m.busy() || m.jobs[0].state != "failed" || m.jobs[1].state != "completed" || len(m.jobs[0].result.Remaining) != 1 {
		t.Fatal("failure wedged queue or lost retry resource")
	}
}
func TestJobWorkspaceTargetsAndSelectionFrozen(t *testing.T) {
	b := &jobBackend{snapshot: managementFixture()}
	m := NewDashboard(b, config.DefaultCatalog(), "")
	defer m.cancel()
	s := b.snapshot
	s.Workspaces[0].Selection = &config.Selection{Schema: 1, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}, Utilities: []string{"rtk"}}
	s.Workspaces[0].Sandboxes[1].Active = false
	deliver(m, s)
	m.chooseAction(m.actions()[1])
	s.Workspaces[0].Selection.Utilities[0] = "replacement"
	s.Workspaces[0].Selection.Pairs[0].Agent = "claude"
	s.Workspaces[0].Sandboxes[0].Target.Entry.Sandbox = "new"
	s.Workspaces[0].Sandboxes[1].Active = true
	deliver(m, s)
	runJobCommand(m, keyCmd(m, "enter"))
	b.mu.Lock()
	defer b.mu.Unlock()
	r := b.requests[0]
	if len(r.Targets) != 1 || r.Targets[0].Entry.Sandbox != "sandbox-one" || r.Selection.Pairs[0].Agent != "codex" || r.Selection.Utilities[0] != "rtk" {
		t.Fatalf("workspace confirmation was mutable: %+v", r)
	}
}
func TestCancelAndExit(t *testing.T) {
	b := &jobBackend{snapshot: managementFixture(), execute: func(ctx context.Context, r operations.Request, _ func(operations.Event)) (operations.Result, error) {
		<-ctx.Done()
		return operations.Result{Changed: true, NeedsVerification: true, Remaining: []operations.Target{r.Target}}, ctx.Err()
	}}
	m := NewDashboard(b, config.DefaultCatalog(), "")
	defer m.cancel()
	deliver(m, b.snapshot)
	events := startJobCommands(m.enqueue(operations.Request{Kind: operations.KindUpdate, Target: m.rows()[0].Target}))
	m.enqueue(operations.Request{Kind: operations.KindStop, Target: m.rows()[1].Target})
	if cmd := keyCmd(m, "q"); cmd != nil || !m.exitPrompt {
		t.Fatal("busy q exited without decision")
	}
	keyCmd(m, "k")
	if m.ctx.Err() != nil || m.exitPrompt {
		t.Fatal("keep running cancelled worker")
	}
	keyCmd(m, "ctrl+c")
	keyCmd(m, "c")
	if m.jobs[0].state != "cancel requested" || m.jobs[1].state != "cancelled" || !strings.Contains(m.logContent(), "취소 요청") {
		t.Fatal("cancellation was not shown immediately")
	}
	done := awaitJobDone(t, events)
	_, quit := m.Update(done)
	if quit == nil || m.jobs[0].state != "needs verification" || !strings.Contains(m.logContent(), "guest 중단/부분 설치 확인 필요") {
		t.Fatal("unverified guest cancellation overclaimed")
	}
	if _, ok := quit().(tea.QuitMsg); !ok {
		t.Fatal("cancel and wait never exited")
	}
	idle := NewDashboard(b, config.DefaultCatalog(), "")
	if cmd := keyCmd(idle, "q"); cmd == nil {
		t.Fatal("idle q did not quit")
	}
}
func TestJobCompletionRequestsFreshSnapshotEvenDuringObservation(t *testing.T) {
	b := &jobBackend{snapshot: managementFixture()}
	m := NewDashboard(b, config.DefaultCatalog(), "")
	defer m.cancel()
	deliver(m, b.snapshot)
	stale := m.refresh(time.Now(), false)
	events := startJobCommands(m.enqueue(operations.Request{Kind: operations.KindStop, Target: m.rows()[0].Target}))
	m.Update(awaitJobDone(t, events))
	_, fresh := m.Update(stale())
	if fresh == nil {
		t.Fatal("completion was lost behind in-flight snapshot")
	}
	if _, ok := fresh().(snapshotMsg); !ok {
		t.Fatal("completion scheduled timer rather than a fresh snapshot")
	}
}
func TestJobFinishedPulseDoesNotSpin(t *testing.T) {
	b := &jobBackend{snapshot: managementFixture()}
	m := NewDashboard(b, config.DefaultCatalog(), "")
	defer m.cancel()
	deliver(m, b.snapshot)
	cmd := m.enqueue(operations.Request{Kind: operations.KindStop, Target: m.rows()[0].Target})
	batch := cmd().(tea.BatchMsg)
	done := batch[0]()
	pulse := batch[1]()
	_, next := m.Update(pulse)
	if next != nil {
		t.Fatal("finished worker spawned another immediate pulse before completion delivery")
	}
	m.Update(done)
}
func TestBoundedSafeLogs(t *testing.T) {
	l := newJobLog()
	for _, p := range []string{"한"[:1], "한"[1:], "\x1b[", "31m빨강\x1b[0m", "\x1b]52;c;SECRET", "\x1b", "\\끝", "\x1b(", "B\x00\r"} {
		l.emit(operations.Event{Stage: "stdout", Text: p})
	}
	if got := l.text(); got != "한빨강끝" {
		t.Fatalf("fragmented controls/UTF-8 leaked: %q", got)
	}
	deadline := make(chan struct{})
	go func() {
		for i := 0; i < 500; i++ {
			l.emit(operations.Event{Stage: "stdout", Text: strings.Repeat("한국어", 1000)})
		}
		close(deadline)
	}()
	select {
	case <-deadline:
	case <-time.After(2 * time.Second):
		t.Fatal("slow/unread view blocked log writer")
	}
	if got := l.text(); len(got) > 256<<10 || !utf8.ValidString(got) || strings.ContainsAny(got, "\x1b\x00\r") {
		t.Fatal("unsafe or oversized retained log")
	}
	m := NewDashboard(&jobBackend{}, config.DefaultCatalog(), "")
	defer m.cancel()
	for i := 0; i < 25; i++ {
		runJobCommand(m, m.enqueue(operations.Request{Kind: operations.KindStop}))
	}
	if len(m.jobs) > 20 {
		t.Fatal("completed history exceeded bound")
	}
	active := NewDashboard(&jobBackend{}, config.DefaultCatalog(), "")
	defer active.cancel()
	for i := 0; i < 21; i++ {
		active.enqueue(operations.Request{Kind: operations.KindStop})
	}
	if len(active.jobs) != 20 || !strings.Contains(active.actionError, "가득") {
		t.Fatal("active queue exceeded bound")
	}
}

func TestBoundedSafeLogsConcurrentStreams(t *testing.T) {
	log := newJobLog()
	var writers sync.WaitGroup
	writers.Add(2)
	for _, stream := range []struct{ stage, text string }{{"stdout", "한"}, {"stderr", "글"}} {
		go func(stage, text string) {
			defer writers.Done()
			for i := 0; i < 500; i++ {
				log.emit(operations.Event{Stage: stage, Text: text[:1]})
				log.emit(operations.Event{Stage: stage, Text: text[1:]})
			}
		}(stream.stage, stream.text)
	}
	done := make(chan struct{})
	go func() { writers.Wait(); close(done) }()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-done:
			got := log.text()
			if strings.Count(got, "한") != 500 || strings.Count(got, "글") != 500 {
				t.Fatalf("streams mixed fragmented runes: %q", got)
			}
			return
		case <-timer.C:
			t.Fatal("concurrent writers/readers stalled")
		default:
			if got := log.text(); !utf8.ValidString(got) || len(got) > 256<<10 {
				t.Fatal("concurrent view observed torn or oversized data")
			}
		}
	}
}

func TestBoundedSafeLogsSanitizeResultMetadata(t *testing.T) {
	m := NewDashboard(&jobBackend{}, config.DefaultCatalog(), "")
	defer m.cancel()
	m.enqueue(operations.Request{Kind: operations.KindVersions})
	j := m.jobs[0]
	m.Update(jobDoneMsg{id: j.id, result: operations.Result{Versions: map[string]string{"agent": "\x1b]52;c;SECRET\av1"}, VersionErrors: map[string]string{"utility:rtk": "\x1b[31mprobe failed\x1b[0m"}}})
	if got := m.logContent(); strings.ContainsAny(got, "\x1b\a") || strings.Contains(got, "SECRET") || !strings.Contains(got, "v1") || !strings.Contains(got, "probe failed") {
		t.Fatalf("guest result metadata bypassed log safety: %q", got)
	}
}
func TestShellReturn(t *testing.T) {
	b := &jobBackend{snapshot: managementFixture()}
	m := NewDashboard(b, config.DefaultCatalog(), "")
	defer m.cancel()
	deliver(m, b.snapshot)
	m.enqueue(operations.Request{Kind: operations.KindStop, Target: m.rows()[0].Target})
	if cmd := keyCmd(m, "s"); cmd != nil || !strings.Contains(m.actionError, "완료") {
		t.Fatal("shell allowed during mutation")
	}
	m.jobs = nil
	m.runningJob = 0
	cmd := keyCmd(m, "s")
	if cmd == nil {
		t.Fatal("idle shell missing")
	}
	msg := cmd().(shellReadyMsg)
	if b.shellTarget.Entry.Sandbox != "sandbox-one" || len(b.requests) != 0 {
		t.Fatal("shell target changed or auto-updated")
	}
	_, execCmd := m.Update(msg)
	if execCmd == nil || !m.shellRunning {
		t.Fatal("shell did not hand terminal to ExecProcess")
	}
	_, refresh := m.Update(shellDoneMsg{errors.New("child exit")})
	if m.shellRunning || refresh == nil || !strings.Contains(m.actionError, "child exit") {
		t.Fatal("shell callback lost error or refresh")
	}
	m.Update(refresh())
	b.shellErr = errors.New("target changed")
	m.Update(keyCmd(m, "s")())
	if !strings.Contains(m.actionError, "target changed") || m.shellPending {
		t.Fatal("shell preparation error wedged dashboard")
	}
}

func TestShellExecProcessCallback(t *testing.T) {
	if os.Getenv("TUI_SHELL_TEST_HELPER") == "1" {
		os.Exit(7)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	refreshed := make(chan struct{}, 4)
	b := &shellProgramBackend{jobBackend: jobBackend{snapshot: managementFixture()}, exe: exe, refreshed: refreshed}
	m := NewDashboard(b, config.DefaultCatalog(), "")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutRenderer())
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	select {
	case <-refreshed:
	case <-ctx.Done():
		t.Fatal("initial snapshot timed out")
	}
	p.Send(tea.KeyPressMsg{Code: 's', Text: "s"})
	select {
	case <-refreshed:
	case <-ctx.Done():
		t.Fatal("ExecProcess callback did not refresh")
	}
	p.Send(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if m.shellRunning || !strings.Contains(m.actionError, "exit status 7") {
		t.Fatalf("ExecProcess callback lost child error: %q", m.actionError)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.requests) != 0 {
		t.Fatal("ExecProcess auto-updated before shell")
	}
}

type shellProgramBackend struct {
	jobBackend
	exe       string
	refreshed chan struct{}
}

func (b *shellProgramBackend) Snapshot(ctx context.Context, q operations.Query) (operations.Snapshot, error) {
	s, err := b.jobBackend.Snapshot(ctx, q)
	b.refreshed <- struct{}{}
	return s, err
}
func (b *shellProgramBackend) ShellCommand(_ context.Context, target operations.Target) (*exec.Cmd, error) {
	cmd := exec.Command(b.exe, "-test.run=^TestShellExecProcessCallback$")
	cmd.Env = append(os.Environ(), "TUI_SHELL_TEST_HELPER=1")
	b.shellTarget = target
	return cmd, nil
}

func TestJobRetryUsesFailedRequest(t *testing.T) {
	b := &jobBackend{snapshot: managementFixture(), execute: func(context.Context, operations.Request, func(operations.Event)) (operations.Result, error) {
		return operations.Result{}, errors.New("retry me")
	}}
	m := NewDashboard(b, config.DefaultCatalog(), "")
	defer m.cancel()
	deliver(m, b.snapshot)
	original := m.rows()[0].Target
	runJobCommand(m, m.enqueue(operations.Request{Kind: operations.KindStop, Target: original}))
	m.ProfileID = "a-two"
	keyCmd(m, "R")
	if m.confirm == nil || m.confirm.request.Target.Entry.Sandbox != "sandbox-one" {
		t.Fatal("retry lost failed immutable target")
	}
	runJobCommand(m, keyCmd(m, "enter"))
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.requests) != 2 || b.requests[1].Target != original {
		t.Fatal("retry followed current selection")
	}
}

func TestOneMutationRealServiceAndRegistryRevalidation(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprint("changed=", changed), func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HOME", t.TempDir())
			t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
			t.Setenv("TUI_JOB_PROCESS", "1")
			t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
			logPath := filepath.Join(dir, "commands")
			readyPath := filepath.Join(dir, "ready")
			releasePath := filepath.Join(dir, "release")
			t.Setenv("TUI_JOB_COMMANDS", logPath)
			t.Setenv("TUI_JOB_READY", readyPath)
			t.Setenv("TUI_JOB_RELEASE", releasePath)
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			if err = os.Symlink(exe, filepath.Join(dir, "sbx")); err != nil {
				t.Fatal(err)
			}
			workspace, err := config.Canonical(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			runtime, _ := config.RuntimePath(workspace)
			runtime, err = config.Canonical(runtime)
			if err != nil {
				t.Fatal(err)
			}
			reg := config.Registry{Workspace: workspace, Profiles: map[string]config.Entry{"codex-native": {Workspace: workspace, Agent: "codex", Tool: "native", Sandbox: "one", Image: "image"}, "claude-native": {Workspace: workspace, Agent: "claude", Tool: "native", Sandbox: "two", Image: "image"}}, Active: []string{"codex-native", "claude-native"}}
			if err = config.WriteJSON(filepath.Join(runtime, "registry.json"), reg); err != nil {
				t.Fatal(err)
			}
			svc := &operations.Service{Root: dir, Out: io.Discard, Err: io.Discard, Catalog: config.DefaultCatalog()}
			snap, err := svc.Snapshot(context.Background(), operations.Query{})
			if err != nil {
				t.Fatal(err)
			}
			m := NewDashboard(svc, config.DefaultCatalog(), "")
			defer m.cancel()
			deliver(m, snap)
			m.ProfileID = "codex-native"
			one := m.selectedSandbox().Target
			m.ProfileID = "claude-native"
			two := m.selectedSandbox().Target
			events := startJobCommands(m.enqueue(operations.Request{Kind: operations.KindStop, Target: one}))
			end := time.Now().Add(2 * time.Second)
			for {
				if _, err := os.Stat(readyPath); err == nil {
					break
				}
				if time.Now().After(end) {
					t.Fatal("real stop did not start")
				}
				time.Sleep(5 * time.Millisecond)
			}
			if cmd := m.enqueue(operations.Request{Kind: operations.KindStop, Target: two}); cmd != nil {
				t.Fatal("second real stop dispatched early")
			}
			refresh := m.refresh(time.Now(), false)
			m.Update(refresh())
			data, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "stop two") || !strings.Contains(string(data), "ls --json") {
				t.Fatalf("snapshot/mutation interleaving wrong: %s", data)
			}
			if changed {
				entry := reg.Profiles["claude-native"]
				entry.Sandbox = "replacement"
				reg.Profiles["claude-native"] = entry
				if err = config.WriteJSON(filepath.Join(runtime, "registry.json"), reg); err != nil {
					t.Fatal(err)
				}
			}
			if err = os.WriteFile(releasePath, nil, 0600); err != nil {
				t.Fatal(err)
			}
			_, next := m.Update(awaitJobDone(t, events))
			follow := startJobCommands(next)
			m.Update(awaitJobDone(t, follow))
			data, err = os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			if changed {
				if m.jobs[1].state != "failed" || !strings.Contains(m.jobs[1].errorText, "target changed") || strings.Contains(string(data), "stop two") || strings.Contains(string(data), "stop replacement") {
					t.Fatalf("registry change was not rejected: %s / %q", data, m.jobs[1].errorText)
				}
			} else if m.jobs[1].state != "completed" || !strings.Contains(string(data), "stop two") {
				t.Fatal("serialized real stop did not complete")
			}
		})
	}
}

func TestJobConfirmationScrollsOnSmallScreen(t *testing.T) {
	m := NewDashboard(&jobBackend{}, config.DefaultCatalog(), "")
	deliver(m, managementFixture())
	m.Width = 35
	m.Height = 5
	keyCmd(m, "u")
	m.View()
	for i := 0; i < 5; i++ {
		keyCmd(m, "j")
	}
	if view := m.View().Content; !strings.Contains(view, "시작") {
		t.Fatalf("small screen cannot reach start warning: %q", view)
	}
	keyCmd(m, "esc")
	keyCmd(m, "enter")
	for i := 0; i < 6; i++ {
		keyCmd(m, "j")
	}
	if view := m.View().Content; !strings.Contains(view, "Paseo 해제") {
		t.Fatalf("small screen hides selected action: %q", view)
	}
}

func TestJobStagesAreDeliveredByMessages(t *testing.T) {
	b := &jobBackend{}
	m := NewDashboard(b, config.DefaultCatalog(), "")
	defer m.cancel()
	m.enqueue(operations.Request{Kind: operations.KindUpdate})
	j := m.jobs[0]
	j.log.emit(operations.Event{Stage: "install", Text: "installation output"})
	if strings.Contains(m.logContent(), "· install ·") {
		t.Fatal("worker mutated rendered stage before message delivery")
	}
	m.Update(jobPulseMsg{id: j.id})
	if !strings.Contains(m.logContent(), "· install ·") || !strings.Contains(m.logContent(), "⠋") {
		t.Fatal("stage message or progress indicator missing")
	}
}
func TestCancelWordInFailureDoesNotClaimCancellation(t *testing.T) {
	m := NewDashboard(&jobBackend{}, config.DefaultCatalog(), "")
	defer m.cancel()
	m.enqueue(operations.Request{Kind: operations.KindStop})
	j := m.jobs[0]
	m.Update(jobDoneMsg{id: j.id, err: errors.New("server canceled deployment policy check")})
	if j.state != "failed" {
		t.Fatal("unrelated error text claimed confirmed cancellation")
	}
}
