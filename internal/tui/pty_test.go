//go:build darwin || linux

package tui

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/creack/pty"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operations"
)

// The actual dashboard and operations service run in a separate terminal session.
func TestDashboardPTYHelper(t *testing.T) {
	if os.Getenv("DASHBOARD_PTY_HELPER") != "1" {
		return
	}
	service, err := operations.New(os.Getenv("DASHBOARD_PTY_ROOT"))
	if err == nil {
		err = RunDashboard(service, service.Catalog, os.Getenv("DASHBOARD_PTY_WORKSPACE"), os.Stdin, os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("DASHBOARD_EXIT")
	// Keep the session leader alive until the test reads restored termios;
	// macOS detaches the slave when the leader exits (then ioctl is ENOTTY).
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	os.Exit(0)
}

// Go fake commands reject unexpected mutations. Shell exercises real ExecProcess
// ownership; stop emits a flood through the real subprocess/event/log path.
const dashboardFakeSource = `package main
import("bufio";"fmt";"os";"path/filepath";"strings";"github.com/charmbracelet/x/term")
func main(){
 a:=os.Args[1:]; name:=filepath.Base(os.Args[0])
 switch name {
 case "sbx":
  switch {
  case strings.Join(a," ")=="ls --json": fmt.Println("{\"sandboxes\":[{\"name\":\"pty-sandbox\",\"status\":\"running\"}]}")
  case len(a)==6 && a[0]=="exec" && a[1]=="-it" && a[2]=="--workdir" && a[3]==os.Getenv("DASHBOARD_PTY_WORKSPACE") && a[4]=="pty-sandbox" && a[5]=="bash":
   width,height,err:=term.GetSize(os.Stdin.Fd());if err!=nil{panic(err)}
   state,err:=term.GetState(os.Stdin.Fd());if err!=nil{panic(err)}
   fmt.Printf("SHELL_READY %dx%d\n",width,height)
   if line,_:=bufio.NewReader(os.Stdin).ReadString('\n');strings.TrimSpace(line)!="exit"{os.Exit(3)}
   // A terminal application may change modes; restore the inherited state.
   if _,err=term.MakeRaw(os.Stdin.Fd());err!=nil{panic(err)}
   if err=term.Restore(os.Stdin.Fd(),state);err!=nil{panic(err)}
   fmt.Println("SHELL_EXIT")
  case strings.Join(a," ")=="stop pty-sandbox":
   fmt.Print("FLOOD_FIRST\n"+strings.Repeat("한국어 로그\n",200000)+"FLOOD_LAST\n")
  default: fmt.Fprintln(os.Stderr,"unexpected sbx arguments",a);os.Exit(4)
  }
 case "paseo":
  if len(a)!=4||a[0]!="--home"||a[1]!=filepath.Join(os.Getenv("HOME"),".paseo")||strings.Join(a[2:]," ")!="status --json"{fmt.Fprintln(os.Stderr,"unexpected paseo arguments",a);os.Exit(4)}
  fmt.Println("{\"localDaemon\":\"stopped\"}")
 case "docker":
  if len(a)!=3||a[0]!="info"||a[1]!="--format"{os.Exit(4)}
  fmt.Println("{\"ServerVersion\":\"fake\"}")
 default: os.Exit(4)
 }
}`

// Regressions in terminal ownership, shell return refresh, sizing or NO_COLOR
// must fail at observable application boundaries, without screen snapshots.
func TestDashboardPTYRestore(t *testing.T) {
	temp, err := config.Canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(temp, "fake.go")
	if err = os.WriteFile(source, []byte(dashboardFakeSource), 0600); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(temp, "fake")
	build := exec.Command("go", "build", "-o", fake, source)
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake commands: %v: %s", err, out)
	}
	for _, name := range []string{"sbx", "paseo", "docker"} {
		if err = os.Symlink(fake, filepath.Join(temp, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, scenario := range []struct {
		category config.Category
		exitKey  string
	}{{config.CategoryLegacySandbox, "q"}, {config.CategoryLegacySandbox, "\x03"}, {config.CategorySandbox, "q"}, {config.CategorySandbox, "\x03"}} {
		exitKey := scenario.exitKey
		t.Run(fmt.Sprintf("%s-exit-%x", scenario.category, exitKey), func(t *testing.T) {
			home := filepath.Join(temp, fmt.Sprintf("home-%s-%x", scenario.category, exitKey))
			workspace := filepath.Join(temp, fmt.Sprintf("한국어-workspace-%s-%x", scenario.category, exitKey))
			if err := os.MkdirAll(workspace, 0700); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256([]byte(workspace))
			runtime := filepath.Join(home, ".local", "share", "paseo-sbx", fmt.Sprintf("%x", sum)[:10])
			reg := config.Registry{Workspace: workspace, Active: []string{"codex-superpowers"}, Profiles: map[string]config.Entry{"codex-superpowers": {Agent: "codex", Tool: "superpowers", Workspace: workspace, Sandbox: "pty-sandbox", Image: "paseo-sbx/codex-superpowers:test"}}}
			rowLabel := "codex-superpowers"
			if scenario.category == config.CategorySandbox {
				entry := config.Entry{Category: config.CategorySandbox, Agent: "codex", Workspace: workspace, Sandbox: "pty-sandbox", Image: "paseo-sbx/sandbox/codex:test"}
				reg.Schema = 2
				reg.Active = []string{entry.Key()}
				reg.Profiles = map[string]config.Entry{entry.Key(): entry}
				rowLabel = entry.Key()
			}
			if err := config.WriteJSON(filepath.Join(runtime, "registry.json"), reg); err != nil {
				t.Fatal(err)
			}
			master, slave, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			if err = syscall.SetNonblock(int(master.Fd()), true); err != nil {
				t.Fatal(err)
			}
			// NewFile observes nonblocking mode and attaches Go's poller. pty.Open
			// constructs its File before SetNonblock, so using it directly can
			// terminate the reader on EAGAIN before any rendering arrives.
			fd, err := syscall.Dup(int(master.Fd()))
			if err != nil {
				t.Fatal(err)
			}
			master.Close()
			master = os.NewFile(uintptr(fd), "dashboard-pty")
			defer master.Close()
			defer slave.Close()
			if err = pty.Setsize(master, &pty.Winsize{Cols: 120, Rows: 40}); err != nil {
				t.Fatal(err)
			}
			before, err := term.GetState(slave.Fd())
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestDashboardPTYHelper$")
			cmd.Env = ptyEnvironment(map[string]string{"HOME": home, "PATH": temp + string(os.PathListSeparator) + os.Getenv("PATH"), "TERM": "xterm-256color", "NO_COLOR": "1", "DASHBOARD_PTY_HELPER": "1", "DASHBOARD_PTY_ROOT": root, "DASHBOARD_PTY_WORKSPACE": workspace})
			cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
			cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer cmd.Process.Kill()
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			transcript := &ptyTranscript{}
			go func() {
				b := make([]byte, 8192)
				for {
					n, err := master.Read(b)
					if n > 0 {
						transcript.append(b[:n])
					}
					if err != nil {
						return
					}
				}
			}()
			wait := func(mark string, from int) {
				t.Helper()
				deadline := time.Now().Add(10 * time.Second)
				for time.Now().Before(deadline) {
					if strings.Contains(transcript.text()[min(from, transcript.length()):], mark) {
						return
					}
					time.Sleep(10 * time.Millisecond)
				}
				t.Fatalf("terminal did not show %q: %q", mark, transcript.text())
			}
			send := func(s string) {
				t.Helper()
				if _, err := master.Write([]byte(s)); err != nil {
					t.Fatal(err)
				}
			}
			wait(rowLabel, 0)
			during, err := term.GetState(slave.Fd())
			if err != nil {
				t.Fatal(err)
			}
			if reflect.DeepEqual(before, during) {
				t.Fatal("dashboard did not take terminal raw mode")
			}
			// Resize and assert the interactive child sees the resized terminal.
			if err = pty.Setsize(master, &pty.Winsize{Cols: 80, Rows: 24}); err != nil {
				t.Fatal(err)
			}
			if err = cmd.Process.Signal(syscall.SIGWINCH); err != nil {
				t.Fatal(err)
			}
			send("s")
			wait("SHELL_READY 80x24", 0)
			shellState, err := term.GetState(slave.Fd())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, shellState) {
				t.Fatal("shell inherited dashboard raw attributes")
			}
			offset := transcript.length()
			send("exit\n")
			wait("SHELL_EXIT", offset)
			wait("PASEO SBX", offset)
			// Real subprocess output must flow into bounded logs without flooding the PTY.
			offset = transcript.length()
			send("\r")
			wait("guest 버전 확인", offset)
			send("jjj\r")
			wait("Enter 실행", offset)
			send("\r")
			wait("완료", offset)
			send(exitKey)
			wait("DASHBOARD_EXIT", offset)
			after, err := term.GetState(slave.Fd())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("terminal attributes not restored on exit")
			}
			send("ack\n")
			select {
			case err = <-done:
				if err != nil {
					t.Fatalf("dashboard exit: %v: %s", err, transcript.text())
				}
			case <-time.After(10 * time.Second):
				t.Fatal("dashboard did not exit")
			}
			out := transcript.text()
			if !strings.Contains(out, "\x1b[?1049h") || !strings.Contains(out, "\x1b[?1049l") {
				t.Fatal("alternate screen not restored")
			}
			if color := regexp.MustCompile(`\x1b\[(?:[0-9;]*;)?(?:3[0-8]|4[0-8]|9[0-7]|10[0-7])(?:;[0-9;]*)?m`).FindString(out); color != "" {
				t.Fatalf("NO_COLOR emitted styling %q", color)
			}
			if !strings.Contains(out, "한국어 로그") {
				t.Fatal("subprocess output did not reach the dashboard log")
			}
			if len(out) > 40000 {
				t.Fatalf("flood escaped terminal viewport: %d bytes", len(out))
			}
			t.Logf("PTY 120x40 → 80x24, shell canonical attributes and redraw, flood bounds, NO_COLOR, exit restored; %d output bytes", len(out))
		})
	}
}

func ptyEnvironment(overrides map[string]string) []string {
	env := []string{}
	for _, v := range os.Environ() {
		key, _, _ := strings.Cut(v, "=")
		if _, ok := overrides[key]; !ok {
			env = append(env, v)
		}
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}

type ptyTranscript struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (p *ptyTranscript) append(b []byte) { p.mu.Lock(); defer p.mu.Unlock(); p.data.Write(b) }
func (p *ptyTranscript) text() string    { p.mu.Lock(); defer p.mu.Unlock(); return p.data.String() }
func (p *ptyTranscript) length() int     { p.mu.Lock(); defer p.mu.Unlock(); return p.data.Len() }
