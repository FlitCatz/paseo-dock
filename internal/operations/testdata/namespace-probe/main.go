//go:build linux

// namespace-probe asserts confinement inside the disposable Codex sandbox.
package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func run() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("mode and workspace required")
	}
	mode, workspace := os.Args[1], os.Args[2]
	if !strings.HasPrefix(workspace, "/private/tmp/paseo-sbx-userns-live-") || filepath.Base(workspace) != "workspace" {
		return fmt.Errorf("invalid workspace")
	}
	outside := "/home/agent/namespace-experiment-outside"
	if mode == "prepare" {
		for _, family := range []int{unix.AF_ALG, unix.AF_VSOCK} {
			fd, err := unix.Socket(family, unix.SOCK_STREAM, 0)
			if fd >= 0 {
				unix.Close(fd)
			}
			if err != unix.EPERM {
				return fmt.Errorf("socket family %d wanted EPERM, got %v", family, err)
			}
			fmt.Printf("EXPECTED_SOCKET_FAMILY_DENIED %d\n", family)
		}
		for _, call := range []uintptr{unix.SYS_BPF, unix.SYS_PERF_EVENT_OPEN} {
			_, _, errno := unix.Syscall(call, 0, 0, 0)
			if errno != unix.EPERM {
				return fmt.Errorf("syscall %d wanted EPERM, got %v", call, errno)
			}
			fmt.Printf("EXPECTED_ADMIN_SYSCALL_DENIED %d\n", call)
		}
		if err := unix.Setns(-1, 0); err != unix.EPERM {
			return fmt.Errorf("setns wanted EPERM, got %v", err)
		}
		fmt.Println("EXPECTED_SETNS_DENIED")
		if err := os.MkdirAll(outside, 0700); err != nil {
			return err
		}
		path := filepath.Join(outside, "baseline-write")
		if err := os.WriteFile(path, []byte("baseline\n"), 0600); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		link := filepath.Join(workspace, "outside-link")
		if err := os.Symlink(outside, link); err != nil && !os.IsExist(err) {
			return err
		}
		conn, err := net.DialTimeout("tcp", "1.1.1.1:443", 3*time.Second)
		if err != nil {
			return fmt.Errorf("baseline network unavailable: %w", err)
		}
		conn.Close()
		fmt.Println("BASELINE_OUTSIDE_WRITE_AND_NETWORK_OK")
		return nil
	}
	if mode != "sandbox" {
		return fmt.Errorf("unknown mode")
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return err
	}
	fields := map[string]string{}
	for _, line := range strings.Split(string(status), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok {
			fields[key] = strings.TrimSpace(value)
		}
	}
	for _, expect := range []struct{ key, value string }{{"CapEff", "0000000000000000"}, {"NoNewPrivs", "1"}, {"Seccomp", "2"}} {
		if fields[expect.key] != expect.value {
			return fmt.Errorf("%s missing or unexpected: %q", expect.key, fields[expect.key])
		}
		fmt.Printf("%s: %s\n", expect.key, fields[expect.key])
	}
	if err := os.WriteFile(filepath.Join(workspace, "sandbox-inside"), []byte("workspace-write\n"), 0600); err != nil {
		return fmt.Errorf("workspace write: %w", err)
	}
	for _, path := range []string{filepath.Join(outside, "denied"), filepath.Join(workspace, "outside-link", "denied-symlink"), "/etc/namespace-experiment-denied"} {
		if err := os.WriteFile(path, []byte("unexpected\n"), 0600); err == nil {
			return fmt.Errorf("outside write allowed: %s", path)
		} else {
			fmt.Printf("EXPECTED_WRITE_DENIED %s: %v\n", path, err)
		}
	}
	conn, err := net.DialTimeout("tcp", "1.1.1.1:443", 2*time.Second)
	if err == nil {
		conn.Close()
		return fmt.Errorf("network allowed")
	}
	fmt.Println("EXPECTED_NETWORK_DENIED", err)
	for _, flags := range []int{unix.CLONE_NEWUTS, unix.CLONE_NEWNS, unix.CLONE_NEWPID} {
		if err := unix.Unshare(flags); err != unix.EPERM {
			return fmt.Errorf("unshare %#x: wanted EPERM, got %v", flags, err)
		}
		fmt.Printf("EXPECTED_UNSHARE_DENIED %#x\n", flags)
	}
	if err := unix.Mount("none", "/tmp", "none", 0, ""); err != unix.EPERM {
		return fmt.Errorf("unapproved mount flags: %v", err)
	}
	fmt.Println("CONFINEMENT_ASSERTIONS_PASSED")
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
