package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"paseo-dock/internal/config"
	"paseo-dock/internal/containerpolicy"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUserNSMissingObservedPolicyBlocksStop(t *testing.T) {
	b, e, _, log := dockerFixture(t)
	data, _ := json.Marshal(e)
	var fields map[string]any
	_ = json.Unmarshal(data, &fields)
	fields["security_policy"] = "codex-userns-v1"
	fields["security_policy_digest"] = "de699bf56093ed501d6cf021db43b48f6bd5bd843a715404e7d0714ff0ed0af8"
	data, _ = json.Marshal(fields)
	_ = json.Unmarshal(data, &e)
	if err := b.Stop(context.Background(), e); err == nil {
		t.Error("missing observed policy accepted")
	}
	calls, _ := os.ReadFile(log)
	if strings.Contains(string(calls), `"stop"`) {
		t.Fatal("mutated mismatched policy")
	}
}

func policyDockerFixture(t *testing.T) (*DockerBackend, config.Entry, map[string]any, string) {
	t.Helper()
	b, e, raw, log := dockerFixture(t)
	e.SecurityPolicy = containerpolicy.ID
	e.SecurityPolicyDigest = containerpolicy.SHA256
	labels := raw["Config"].(map[string]any)["Labels"].(map[string]string)
	labels["io.paseo-dock.security-policy"] = containerpolicy.ID
	labels["io.paseo-dock.security-policy-digest"] = containerpolicy.SHA256
	profile, err := containerpolicy.Profile()
	if err != nil {
		t.Fatal(err)
	}
	host := raw["HostConfig"].(map[string]any)
	host["SecurityOpt"] = []string{"no-new-privileges", "seccomp=" + string(profile)}
	host["CapDrop"] = []string{"SYS_ADMIN"}
	writeDockerInspect(t, raw)
	return b, e, raw, log
}
func TestUserNSObservedPolicyMismatchBlocksMutation(t *testing.T) {
	for _, change := range []string{"label", "digest", "json", "missing-nnp", "duplicate-nnp", "extra-option", "capadd", "privileged", "missing-drop", "unknown-policy", "forged-digest", "digest-only", "non-codex", "default-adoption"} {
		t.Run(change, func(t *testing.T) {
			b, e, raw, log := policyDockerFixture(t)
			host := raw["HostConfig"].(map[string]any)
			labels := raw["Config"].(map[string]any)["Labels"].(map[string]string)
			switch change {
			case "label":
				labels["io.paseo-dock.security-policy"] = "other"
			case "digest":
				labels["io.paseo-dock.security-policy-digest"] = "other"
			case "json":
				host["SecurityOpt"] = []string{"no-new-privileges", "seccomp={}"}
			case "missing-nnp":
				host["SecurityOpt"] = host["SecurityOpt"].([]string)[1:]
			case "duplicate-nnp":
				host["SecurityOpt"] = append(host["SecurityOpt"].([]string), "no-new-privileges")
			case "extra-option":
				host["SecurityOpt"] = append(host["SecurityOpt"].([]string), "apparmor=unconfined")
			case "capadd":
				host["CapAdd"] = []string{"NET_ADMIN"}
			case "privileged":
				host["Privileged"] = true
			case "missing-drop":
				host["CapDrop"] = nil
			case "unknown-policy":
				e.SecurityPolicy = "unknown"
			case "forged-digest":
				e.SecurityPolicyDigest = "forged"
			case "digest-only":
				e.SecurityPolicy = ""
			case "non-codex":
				e.Agent = "claude"
			case "default-adoption":
				e.SecurityPolicy = ""
				e.SecurityPolicyDigest = ""
			}
			writeDockerInspect(t, raw)
			if err := b.Stop(context.Background(), e); err == nil {
				t.Error("accepted policy mismatch")
			}
			if err := b.Exec(context.Background(), e, []string{"true"}, nil, io.Discard, io.Discard); err == nil {
				t.Error("accepted exec mismatch")
			}
			calls, _ := os.ReadFile(log)
			for _, mutation := range []string{`"stop"`, `"exec"`} {
				if strings.Contains(string(calls), mutation) {
					t.Fatalf("mutation: %s", calls)
				}
			}
		})
	}
}
func TestUserNSCreatePolicyOptions(t *testing.T) {
	b, e, _, log := policyDockerFixture(t)
	e.ContainerID = ""
	t.Setenv("DOCKER_BACKEND_POLICY", filepath.Join(t.TempDir(), "profile"))
	created, err := b.Create(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	if created.SecurityPolicy != containerpolicy.ID || created.SecurityPolicyDigest != containerpolicy.SHA256 {
		t.Fatal("lost metadata")
	}
	calls := dockerCalls(t, log)
	observed, err := os.ReadFile(os.Getenv("DOCKER_BACKEND_POLICY"))
	if err != nil || !containerpolicy.MatchesJSON(observed) {
		t.Fatalf("Docker received incorrect policy file: %v", err)
	}
	var command []string
	decoder := json.NewDecoder(strings.NewReader(calls))
	for decoder.Decode(&command) == nil {
		for _, arg := range command {
			if strings.HasPrefix(arg, "seccomp=") {
				if _, err := os.Stat(strings.TrimPrefix(arg, "seccomp=")); !os.IsNotExist(err) {
					t.Fatal("temporary policy was not cleaned")
				}
			}
		}
	}
	for _, want := range []string{`"--security-opt","seccomp=`, `"--security-opt","no-new-privileges"`, `"--cap-drop","SYS_ADMIN"`, `io.paseo-dock.security-policy=codex-userns-v1`, `io.paseo-dock.security-policy-digest=` + containerpolicy.SHA256, `"--entrypoint","/bin/sh"`, `/usr/sbin/groupmod`, `/usr/sbin/usermod`, `/usr/bin/chown`, `/usr/sbin/runuser`, `/usr/bin/sleep`} {
		if !strings.Contains(calls, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(calls, `"--cap-add"`) || strings.Contains(calls, `"--privileged"`) {
		t.Fatal("unsafe options")
	}
}
func TestUserNSUnsupportedImageArchitecture(t *testing.T) {
	b, e, _, log := policyDockerFixture(t)
	e.ContainerID = ""
	t.Setenv("DOCKER_BACKEND_ARCH", "riscv64")
	if _, err := b.Create(context.Background(), e); err == nil {
		t.Error("unsupported architecture accepted")
	}
	if strings.Contains(dockerCalls(t, log), `"run"`) {
		t.Fatal("created unsupported guest")
	}
}

func TestUserNSServiceMixedSelectionAndSavedPolicy(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			s, r, path := restoreFixture(t)
			backend := &categoryBackend{}
			if failed {
				backend.createErr = errors.New("startup failed")
			}
			s.Backends = map[config.Category]Backend{config.CategoryContainer: backend}
			selection := config.Selection{Schema: 2, Category: config.CategoryContainer, CodexUserNS: true, Pairs: []config.Pair{{Agent: "claude", Methodology: "superpowers"}, {Agent: "codex", Methodology: "superpowers"}}}
			if failed {
				selection.Pairs = selection.Pairs[1:]
			}
			result, err := s.Execute(context.Background(), Request{Kind: KindCreate, Target: Target{Workspace: r.Target.Workspace}, Selection: selection}, nil)
			if !failed && err != nil {
				t.Fatal(err)
			}
			if failed && err == nil {
				t.Fatal("missing failure")
			}
			var reg config.Registry
			if err := config.ReadJSON(path, &reg); err != nil {
				t.Fatal(err)
			}
			codex := reg.Profiles["container/codex-superpowers"]
			if codex.SecurityPolicy != containerpolicy.ID || codex.SecurityPolicyDigest != containerpolicy.SHA256 || codex.ContainerID == "" {
				t.Fatalf("lost opted-in identity: %+v", codex)
			}
			if !failed {
				claude := reg.Profiles["container/claude-superpowers"]
				if claude.SecurityPolicy != "" || claude.SecurityPolicyDigest != "" {
					t.Fatalf("non-codex opted in: %+v", claude)
				}
				if reg.Selection == nil || !reg.Selection.CodexUserNS {
					t.Fatal("lost saved opt-in")
				}
			} else {
				if !result.NeedsVerification || reg.IsActive(codex.Key()) {
					t.Fatal("failure activated or lacks recovery")
				}
				found := false
				for _, target := range result.Remaining {
					found = found || target.Entry.SecurityPolicy == containerpolicy.ID
				}
				if !found {
					t.Fatal("lost policy in remaining")
				}
			}
		})
	}
}
func TestUserNSServiceRejectsExistingPolicyChange(t *testing.T) {
	for _, previous := range []bool{false, true} {
		t.Run(fmt.Sprint(previous), func(t *testing.T) {
			s, r, path := restoreFixture(t)
			backend := &categoryBackend{}
			s.Backends = map[config.Category]Backend{config.CategoryContainer: backend}
			selection := config.Selection{Schema: 2, Category: config.CategoryContainer, CodexUserNS: previous, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}}
			if _, err := s.Execute(context.Background(), Request{Kind: KindCreate, Target: Target{Workspace: r.Target.Workspace}, Selection: selection}, nil); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			backend.calls = nil
			selection.CodexUserNS = !previous
			if _, err := s.Execute(context.Background(), Request{Kind: KindCreate, Target: Target{Workspace: r.Target.Workspace}, Selection: selection}, nil); err == nil {
				t.Fatal("existing resource policy changed")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) || len(backend.calls) > 0 {
				t.Fatalf("mutated existing resource: %v", backend.calls)
			}
		})
	}
}
func TestUserNSBakeContentsUnchanged(t *testing.T) {
	a := imageTestExecution(t, config.Selection{Schema: 2, Category: config.CategoryContainer, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}})
	before, _ := json.Marshal(a.BakeConfig("test", "linux/arm64"))
	a.Selection.CodexUserNS = true
	after, _ := json.Marshal(a.BakeConfig("test", "linux/arm64"))
	if !bytes.Equal(before, after) {
		t.Fatalf("runtime option changed image contents: %s %s", before, after)
	}
}

func TestUserNSDockerPartialCreationRetainsPolicy(t *testing.T) {
	for _, mode := range []string{"failed", "canceled", "readiness"} {
		t.Run(mode, func(t *testing.T) {
			b, e, _, log := policyDockerFixture(t)
			e.ContainerID = ""
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "failed" {
				t.Setenv("DOCKER_BACKEND_RUN_FAIL", "1")
			} else {
				signal := filepath.Join(t.TempDir(), "boundary")
				if mode == "canceled" {
					t.Setenv("DOCKER_BACKEND_RUN_SIGNAL", signal)
				} else {
					t.Setenv("DOCKER_BACKEND_EXEC_FAIL", "1")
					t.Setenv("DOCKER_BACKEND_READY_SIGNAL", signal)
				}
				go func() {
					for {
						if _, err := os.Stat(signal); err == nil {
							cancel()
							return
						}
						select {
						case <-ctx.Done():
							return
						case <-time.After(10 * time.Millisecond):
						}
					}
				}()
			}
			created, err := b.Create(ctx, e)
			if err == nil || created.ContainerID != strings.Repeat("a", 64) || created.SecurityPolicy != containerpolicy.ID || created.SecurityPolicyDigest != containerpolicy.SHA256 {
				t.Fatalf("lost partial policy identity: %+v %v", created, err)
			}
			if mode != "failed" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
			calls := dockerCalls(t, log)
			if strings.Contains(calls, `"stop"`) || strings.Contains(calls, `"rm"`) {
				t.Fatalf("mutated partial resource: %s", calls)
			}
		})
	}
}
func TestUserNSDockerRestartKeepsPolicy(t *testing.T) {
	b, e, raw, log := policyDockerFixture(t)
	raw["State"] = map[string]any{"Running": false}
	writeDockerInspect(t, raw)
	created, err := b.Create(context.Background(), e)
	if err != nil || !created.EqualIdentity(e) {
		t.Fatalf("restart identity: %+v %v", created, err)
	}
	calls := dockerCalls(t, log)
	if !strings.Contains(calls, `"start","`+e.ContainerID+`"`) || strings.Contains(calls, `"run"`) {
		t.Fatalf("restart recreated target: %s", calls)
	}
}
func TestUserNSDockerSupportedArchitectures(t *testing.T) {
	for _, arch := range []string{"arm64", "amd64"} {
		t.Run(arch, func(t *testing.T) {
			b, e, _, _ := policyDockerFixture(t)
			e.ContainerID = ""
			t.Setenv("DOCKER_BACKEND_ARCH", arch)
			if _, err := b.Create(context.Background(), e); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestUserNSPolicylessDaemonOptionsRemainCompatible(t *testing.T) {
	b, e, raw, _ := dockerFixture(t)
	raw["HostConfig"].(map[string]any)["SecurityOpt"] = []string{"seccomp=default"}
	writeDockerInspect(t, raw)
	if _, err := b.Inspect(context.Background(), e); err != nil {
		t.Fatal(err)
	}
}

func TestUserNSDockerCapabilityDropAliases(t *testing.T) {
	for _, tc := range []struct {
		drop  string
		valid bool
	}{
		{"SYS_ADMIN", true}, {"CAP_SYS_ADMIN", true},
		{"sys_admin", false}, {"cap_sys_admin", false},
		{"CAP_SYS_ADMIN_EXTRA", false}, {"SYS_ADMIN_EXTRA", false},
		{"CAP_CAP_SYS_ADMIN", false}, {"CAP_SYS_ADMIN ", false},
		{" CAP_SYS_ADMIN", false}, {"CAP_NET_ADMIN", false},
	} {
		t.Run(tc.drop, func(t *testing.T) {
			b, e, raw, log := policyDockerFixture(t)
			raw["HostConfig"].(map[string]any)["CapDrop"] = []string{tc.drop}
			writeDockerInspect(t, raw)
			if tc.valid {
				if _, err := b.Inspect(context.Background(), e); err != nil {
					t.Fatalf("valid capability drop rejected: %v", err)
				}
				e.ContainerID = ""
				created, err := b.Create(context.Background(), e)
				if err != nil {
					t.Fatalf("creation verification rejected Docker capability spelling: %v", err)
				}
				if err := b.Stop(context.Background(), created); err != nil {
					t.Fatalf("cleanup verification rejected Docker capability spelling: %v", err)
				}
				return
			}
			if err := b.Stop(context.Background(), e); err == nil {
				t.Error("invalid drop permitted stop")
			}
			if err := b.Exec(context.Background(), e, []string{"true"}, nil, io.Discard, io.Discard); err == nil {
				t.Error("invalid drop permitted exec")
			}
			calls := dockerCalls(t, log)
			if strings.Contains(calls, `"stop"`) || strings.Contains(calls, `"exec"`) {
				t.Fatalf("invalid capability spelling reached mutation: %s", calls)
			}
		})
	}
}
