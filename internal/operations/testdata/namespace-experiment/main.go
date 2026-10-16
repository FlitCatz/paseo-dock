// namespace-experiment is an opt-in spike, never used by the product runtime.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const image = "sha256:7760a63ba6a62611c4434c1208c1c87236cd37d36b691da206ebd0d7e77d6521"
const baselineSHA = "6416b47770785a41ac59073cdc77d9fe98517df2799dc83ef207e622de3053f6"

func profile() ([]byte, error) {
	b, err := os.ReadFile("/private/tmp/paseo-dock-docker-pinned-seccomp.json")
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != baselineSHA {
		return nil, fmt.Errorf("baseline digest mismatch")
	}
	var p map[string]json.RawMessage
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	var rules []map[string]interface{}
	if err := json.Unmarshal(p["syscalls"], &rules); err != nil {
		return nil, err
	}
	for _, rule := range rules {
		names := rule["names"].([]interface{})
		filtered := names[:0]
		for _, name := range names {
			if name != "socketcall" {
				filtered = append(filtered, name)
			}
		}
		rule["names"] = filtered
	}
	allow := func(name string, index int, value uint64) {
		rules = append(rules, map[string]interface{}{"names": []string{name}, "action": "SCMP_ACT_ALLOW", "args": []map[string]interface{}{{"index": index, "value": value, "op": "SCMP_CMP_EQ"}}})
	}
	// Exact bwrap clone combinations: user and mount mandatory, optional IPC/PID/network.
	for bits := uint64(0); bits < 8; bits++ {
		flags := uint64(17 | 0x10000000 | 0x20000)
		for bit, ns := range []uint64{0x08000000, 0x20000000, 0x40000000} {
			if bits&(1<<bit) != 0 {
				flags |= ns
			}
		}
		allow("clone", 0, flags)
	}
	allow("unshare", 0, 0x10000000)
	// Exact source-derived propagation, tmpfs/proc/devpts and bind flags.
	for _, flags := range []uint64{0x8000 | 0x80000 | 0x4000, 2 | 4, 2 | 4 | 8, 2 | 8, 0x8000 | 0xc0ed0000 | 0x1000 | 0x4000, 0x8000 | 0x4000 | 0x40000, 0x8000 | 0x1000, 0x8000 | 0x1000 | 0x4000} {
		allow("mount", 3, flags)
	}
	// Remount only bind+remount+silent with known inherited safety/access flags.
	for bits := uint64(0); bits < 64; bits++ {
		flags := uint64(0x8000 | 0x1000 | 0x20 | 2)
		for bit, flag := range []uint64{1, 4, 8, 0x400, 0x800, 0x200000} {
			if bits&(1<<bit) != 0 {
				flags |= flag
			}
		}
		allow("mount", 3, flags)
	}
	allow("umount2", 1, 2)
	rules = append(rules, map[string]interface{}{"names": []string{"pivot_root"}, "action": "SCMP_ACT_ALLOW"})
	p["syscalls"], err = json.Marshal(rules)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(p, "", "  ")
}

func run() (retErr error) {
	existing := flag.String("root", "", "resume the same disposable experiment container")
	profileOut := flag.String("profile-out", "", "write the minimal candidate only, without creating a container")
	probeBin := flag.String("probe", "", "previously built Linux arm64 Go confinement probe for a new workspace")
	flag.Parse()
	b, err := profile()
	if err != nil {
		return err
	}
	if *profileOut != "" {
		if *existing != "" || filepath.Dir(*profileOut) != "/private/tmp" {
			return fmt.Errorf("profile output must be a file in /private/tmp")
		}
		if err := os.WriteFile(*profileOut, b, 0600); err != nil {
			return err
		}
		fmt.Printf("MINIMAL_CANDIDATE %s SHA256=%x\n", *profileOut, sha256.Sum256(b))
		return nil
	}
	root := *existing
	if root == "" {
		root, err = os.MkdirTemp("/private/tmp", "paseo-dock-userns-live-")
		if err != nil {
			return err
		}
	} else if filepath.Dir(root) != "/private/tmp" || !strings.HasPrefix(filepath.Base(root), "paseo-dock-userns-live-") {
		return fmt.Errorf("invalid experiment root")
	}
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0700); err != nil && (*existing == "" || !os.IsExist(err)) {
		return err
	}
	if *existing == "" {
		data, err := os.ReadFile(*probeBin)
		if err != nil {
			return fmt.Errorf("prepare probe before create: %w", err)
		}
		binary, err := elf.NewFile(bytes.NewReader(data))
		if err != nil {
			return err
		}
		if binary.Machine != elf.EM_AARCH64 || binary.Class != elf.ELFCLASS64 {
			return fmt.Errorf("probe must target pinned Linux arm64 image")
		}
		if err := os.WriteFile(filepath.Join(workspace, "namespace-probe"), data, 0755); err != nil {
			return err
		}
	} else if _, err := os.Stat(filepath.Join(workspace, "namespace-probe")); err != nil {
		return err
	}
	path := filepath.Join(root, "seccomp.json")
	if *existing == "" {
		if err := os.WriteFile(path, b, 0600); err != nil {
			return err
		}
	} else {
		prior, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(prior, b) {
			// The sole created spike container used revision one. Preserve and test
			// its exact policy; never recreate it or silently substitute a profile.
			sum := sha256.Sum256(prior)
			if hex.EncodeToString(sum[:]) != "cb1ec6ebd5e65e1852a77a4de8e8911c3a6f50827eaeaf0df6fd9d683a371d06" {
				return fmt.Errorf("preserved candidate differs")
			}
			fmt.Println("REVISION_ONE_POLICY: includes unneeded strict/lazy timestamp remount combinations; minimal revision remains untested")
			b = prior
		}
	}
	sum := sha256.Sum256(b)
	fmt.Printf("EXPERIMENT_ROOT %s\nBASELINE_SHA256 %s\nCANDIDATE_SHA256 %x\n", root, baselineSHA, sum)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "create", "--name", filepath.Base(root), "--label", "io.paseo-dock.experiment="+filepath.Base(root), "--network", "bridge", "--security-opt", "seccomp="+path, "--security-opt", "no-new-privileges=true", "--mount", "type=bind,src="+workspace+",dst="+workspace, "--workdir", workspace, "--user", "root", "--env", "HOME=/home/agent", "--entrypoint", "/usr/bin/sh", image, "-c", "set -eu; /usr/sbin/groupmod -o -g 20 \"$(/usr/bin/id -gn agent)\"; /usr/sbin/usermod -o -u 501 -g 20 agent; /usr/bin/chown -R 501:20 /home/agent; /usr/bin/touch /tmp/paseo-dock-agent-ready; exec /usr/sbin/runuser -u agent -- /usr/bin/sleep infinity")
	var out []byte
	if *existing == "" {
		out, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("create: %w: %s", err, out)
		}
	} else {
		out, err = os.ReadFile(filepath.Join(root, "container-id"))
		if err != nil {
			return err
		}
	}
	cid := strings.TrimSpace(string(out))
	if len(cid) != 64 {
		return fmt.Errorf("invalid returned container identity")
	}
	if err := os.WriteFile(filepath.Join(root, "container-id"), []byte(cid+"\n"), 0600); err != nil {
		return err
	}
	fmt.Println("EXACT_CONTAINER", cid)
	guard := func(c context.Context) error {
		out, err := exec.CommandContext(c, "docker", "inspect", cid).Output()
		if err != nil {
			return err
		}
		var states []struct {
			ID         string
			Image      string
			Config     struct{ Labels map[string]string }
			HostConfig struct {
				Privileged  bool
				CapAdd      []string
				SecurityOpt []string
				NetworkMode string
			}
			Mounts []struct {
				Source      string
				Destination string
				RW          bool
			}
		}
		if err := json.Unmarshal(out, &states); err != nil {
			return err
		}
		if len(states) != 1 {
			return fmt.Errorf("inspect count")
		}
		s := states[0]
		if s.ID != cid || s.Image != image || s.Config.Labels["io.paseo-dock.experiment"] != filepath.Base(root) || s.HostConfig.Privileged || len(s.HostConfig.CapAdd) != 0 || s.HostConfig.NetworkMode != "bridge" || len(s.Mounts) != 1 || s.Mounts[0].Source != workspace || s.Mounts[0].Destination != workspace || !s.Mounts[0].RW {
			return fmt.Errorf("identity/policy mismatch")
		}
		// Engine inspect may serialize the loaded JSON instead of the filename.
		if len(s.HostConfig.SecurityOpt) != 2 {
			return fmt.Errorf("seccomp policy mismatch")
		}
		var seccompOK, privilegesOK bool
		for _, opt := range s.HostConfig.SecurityOpt {
			if opt == "no-new-privileges=true" {
				privilegesOK = true
			}
			if strings.HasPrefix(opt, "seccomp=") {
				actual := strings.TrimPrefix(opt, "seccomp=")
				if actual == path {
					fresh, err := os.ReadFile(path)
					seccompOK = err == nil && bytes.Equal(fresh, b)
				} else {
					var compactActual, compactExpected bytes.Buffer
					seccompOK = json.Compact(&compactActual, []byte(actual)) == nil && json.Compact(&compactExpected, b) == nil && bytes.Equal(compactActual.Bytes(), compactExpected.Bytes())
				}
			}
		}
		if !seccompOK || !privilegesOK {
			return fmt.Errorf("loaded security policy mismatch")
		}
		return nil
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := guard(c); err != nil {
			fmt.Fprintln(os.Stderr, "CLEANUP GUARD", err)
			retErr = errors.Join(retErr, fmt.Errorf("cleanup guard: %w", err))
			return
		}
		out, err := exec.CommandContext(c, "docker", "stop", cid).CombinedOutput()
		fmt.Printf("EXACT_STOP %s %v %s\n", cid, err, out)
		if err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("stop: %w", err))
			return
		}
		state, err := exec.CommandContext(c, "docker", "inspect", "--format", "{{.State.Running}}", cid).Output()
		if err != nil || strings.TrimSpace(string(state)) != "false" {
			retErr = errors.Join(retErr, fmt.Errorf("final stopped state missing: %v", err))
			return
		}
		fmt.Println("FINAL_RUNNING_FALSE", cid)
	}()
	if err := guard(ctx); err != nil {
		return err
	}
	if out, err := exec.CommandContext(ctx, "docker", "start", cid).CombinedOutput(); err != nil {
		return fmt.Errorf("start: %w: %s", err, out)
	}
	for i := 0; i < 50; i++ {
		if exec.CommandContext(ctx, "docker", "exec", cid, "test", "-f", "/tmp/paseo-dock-agent-ready").Run() == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	probe := filepath.Join(workspace, "namespace-probe")
	for _, args := range [][]string{{"id"}, {"bwrap", "--unshare-user", "--ro-bind", "/", "/", "--", "true"}, {"codex", "--version"}, {"codex", "sandbox", "--help"}, {probe, "prepare", workspace}, {"codex", "sandbox", "-c", "sandbox_mode=\"workspace-write\"", "--", probe, "sandbox", workspace}, {"npm", "install", "--global", "--prefix", "/home/agent/.local", "@openai/codex@0.161.0"}, {"/home/agent/.local/bin/codex", "--version"}, {"/home/agent/.local/bin/codex", "sandbox", "-c", "sandbox_mode=\"workspace-write\"", "--", probe, "sandbox", workspace}} {
		if err := guard(ctx); err != nil {
			return err
		}
		fmt.Printf("PROBE %q\n", args)
		cmd := exec.CommandContext(ctx, "docker", append([]string{"exec", "--user", "501:20", cid}, args...)...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("probe failed: %w", err)
		}
	}
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
