package process

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestProcessHelper(t *testing.T) {
	switch os.Getenv("PROCESS_HELPER") {
	case "child":
		ready := os.NewFile(3, "child-ready")
		fmt.Fprintln(ready, os.Getpid())
		ready.Close()
		time.Sleep(10 * time.Second)
		os.Exit(0)
	case "parent":
		read, write, err := os.Pipe()
		if err != nil {
			os.Exit(1)
		}
		cmd := exec.Command(os.Args[0], "-test.run=TestProcessHelper")
		cmd.Env = append(os.Environ(), "PROCESS_HELPER=child")
		cmd.Stdout = os.Stdout
		cmd.ExtraFiles = []*os.File{write}
		if err := cmd.Start(); err != nil {
			os.Exit(1)
		}
		write.Close()
		ready, err := io.ReadAll(read)
		if err != nil || string(bytes.TrimSpace(ready)) != strconv.Itoa(cmd.Process.Pid) {
			os.Exit(2)
		}
		fmt.Print(string(ready))
		cmd.Wait()
		os.Exit(0)
	}
}

// A surviving descendant holding stdout must not keep Wait blocked after cancellation.
func TestProcessCancellationKillsGroup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := Command(ctx, os.Args[0], "-test.run=TestProcessHelper")
	cmd.Env = append(os.Environ(), "PROCESS_HELPER=parent")
	out := &readyOutput{ready: make(chan int, 1)}
	cmd.Stdout = out
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	var pid int
	select {
	case pid = <-out.ready:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("child never acknowledged readiness/PID")
	}
	if pid <= 0 {
		t.Fatal("missing child PID")
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	group, err := syscall.Getpgid(pid)
	if err != nil || group != cmd.Process.Pid {
		t.Fatalf("child not in commanded process group: pid=%d group=%d parent=%d err=%v", pid, group, cmd.Process.Pid, err)
	}
	start := time.Now()
	cancel()
	select {
	case err = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not finish")
	}
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("cancellation succeeded unexpectedly")
	}
	if elapsed > time.Second {
		t.Fatalf("child held stdout after cancellation: %s", elapsed)
	}
}

type readyOutput struct {
	data  bytes.Buffer
	ready chan int
}

func (w *readyOutput) Write(p []byte) (int, error) {
	n, err := w.data.Write(p)
	if bytes.Contains(w.data.Bytes(), []byte("\n")) {
		pid, _ := strconv.Atoi(string(bytes.TrimSpace(w.data.Bytes())))
		select {
		case w.ready <- pid:
		default:
		}
	}
	return n, err
}
