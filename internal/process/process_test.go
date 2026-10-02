package process

import (
	"bytes"
	"context"
	"fmt"
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
		time.Sleep(3 * time.Second)
		os.Exit(0)
	case "parent":
		cmd := exec.Command(os.Args[0], "-test.run=TestProcessHelper")
		cmd.Env = append(os.Environ(), "PROCESS_HELPER=child")
		cmd.Stdout = os.Stdout
		if err := cmd.Start(); err != nil {
			os.Exit(1)
		}
		fmt.Println(cmd.Process.Pid)
		time.Sleep(3 * time.Second)
		os.Exit(0)
	}
}

// A surviving descendant holding stdout must not keep Wait blocked after cancellation.
func TestProcessCancellationKillsGroup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	cmd := Command(ctx, os.Args[0], "-test.run=TestProcessHelper")
	cmd.Env = append(os.Environ(), "PROCESS_HELPER=parent")
	var out bytes.Buffer
	cmd.Stdout = &out
	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)
	pid, _ := strconv.Atoi(string(bytes.TrimSpace(out.Bytes())))
	if pid > 0 {
		defer syscall.Kill(pid, syscall.SIGKILL)
	}
	if err == nil {
		t.Fatal("cancellation succeeded unexpectedly")
	}
	if elapsed > time.Second {
		t.Fatalf("child held stdout after cancellation: %s", elapsed)
	}
}
