package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"paseo-dock/internal/config"
	"paseo-dock/internal/containerpolicy"
	"paseo-dock/internal/operations"
)

func TestCanceledCreationStillEnrollsVerifiedOwnedGuest(t *testing.T) {
	for _, expired := range []bool{false, true} {
		parent, cancel := context.WithCancel(context.Background())
		if expired {
			parent, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		} else {
			cancel()
		}
		defer cancel()
		owned := managedTargetFixture(strings.Repeat("1", 64))
		owner := ownedGuest{cid: owned.Entry.ContainerID}
		var verification context.Context
		verify := ownershipVerifier(parent, func(ctx context.Context, target operations.Target) error {
			verification = ctx
			if !target.EqualIdentity(owned) {
				t.Fatal("wrong target reached verification")
			}
			_, err := capture(ctx, "/usr/bin/true")
			return err
		})
		if err := owner.enroll(owned, verify); err != nil {
			t.Fatalf("canceled creation lost owned cleanup target: %v", err)
		}
		if !owner.target.EqualIdentity(owned) {
			t.Fatal("verified target unavailable to deferred cleanup")
		}
		if _, ok := verification.Deadline(); !ok {
			t.Fatal("ownership verification lacks a bounded deadline")
		}
		if verification.Err() != context.Canceled {
			t.Fatal("verification context not released after inspection")
		}
		foreign := owned
		foreign.Entry.ContainerID = strings.Repeat("2", 64)
		if err := owner.enroll(foreign, verify); err == nil {
			t.Fatal("canceled context allowed foreign CID")
		}
		policyErr := errors.New("policy mismatch")
		if err := owner.enroll(owned, ownershipVerifier(parent, func(context.Context, operations.Target) error { return policyErr })); !errors.Is(err, policyErr) {
			t.Fatalf("cancellation recovery bypassed policy error: %v", err)
		}
		if !owner.target.EqualIdentity(owned) {
			t.Fatal("failed policy inspection changed cleanup target")
		}
	}
}

func TestRejectedPreservedGuestNeverCrossesMutationBoundary(t *testing.T) {
	for _, fixture := range []struct {
		name      string
		mismatch  bool
		policyErr error
	}{
		{"different-valid-managed-CID", true, nil},
		{"policy-inspection-fails", false, errors.New("loaded policy mismatch")},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			ownedCID := strings.Repeat("1", 64)
			candidate := managedTargetFixture(ownedCID)
			if fixture.mismatch {
				candidate.Entry.ContainerID = strings.Repeat("2", 64)
			}
			owner := ownedGuest{cid: ownedCID}
			mutations := []string{}
			verifications := 0
			err := func() error {
				defer func() {
					if owner.target.Entry.ContainerID != "" {
						mutations = append(mutations, "stop "+owner.target.Entry.ContainerID)
					}
				}()
				if err := owner.enroll(candidate, func(got operations.Target) error {
					verifications++
					if !got.EqualIdentity(candidate) {
						t.Fatal("wrong candidate verified")
					}
					return fixture.policyErr
				}); err != nil {
					return err
				}
				mutations = append(mutations, "create", "start", "exec")
				return nil
			}()
			if err == nil {
				t.Fatal("untrusted candidate accepted")
			}
			if len(mutations) != 0 {
				t.Fatalf("rejection crossed mutation boundary: %v", mutations)
			}
			if owner.target.Entry.ContainerID != "" {
				t.Fatal("rejected candidate enrolled in cleanup")
			}
			if fixture.mismatch && verifications != 0 {
				t.Fatal("different CID reached policy inspection")
			}
		})
	}
}

func TestOwnedCIDStaysFrozenAcrossLaterCandidates(t *testing.T) {
	owned := managedTargetFixture(strings.Repeat("1", 64))
	unconfirmed := ownedGuest{}
	if err := unconfirmed.enroll(owned, func(operations.Target) error { return errors.New("policy mismatch") }); err == nil {
		t.Fatal("failed created policy accepted")
	}
	if unconfirmed.cid != "" || unconfirmed.target.Entry.ContainerID != "" {
		t.Fatal("failed created policy froze or enrolled a CID")
	}
	owner := ownedGuest{}
	if err := owner.enroll(owned, func(operations.Target) error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"Service-returned-Remaining-on-resume", "registry-confirmation-after-create"} {
		t.Run(origin, func(t *testing.T) {
			foreign := owned
			foreign.Entry.ContainerID = strings.Repeat("2", 64)
			if err := owner.enroll(foreign, func(operations.Target) error { t.Fatal("foreign CID reached inspection"); return nil }); err == nil {
				t.Fatal("foreign CID accepted")
			}
			if owner.cid != owned.Entry.ContainerID || !owner.target.EqualIdentity(owned) {
				t.Fatal("later candidate changed cleanup ownership")
			}
		})
	}
	changedPolicy := owned
	changedPolicy.Entry.SecurityPolicyDigest = strings.Repeat("0", 64)
	if err := owner.enroll(changedPolicy, func(operations.Target) error { return errors.New("policy mismatch") }); err == nil {
		t.Fatal("wrong policy accepted")
	}
	if !owner.target.EqualIdentity(owned) {
		t.Fatal("failed verification replaced the confirmed cleanup target")
	}
}

func managedTargetFixture(cid string) operations.Target {
	workspace := "/private/tmp/disposable/workspace"
	entry := config.Entry{Category: config.CategoryContainer, Agent: "codex", Tool: "superpowers", Workspace: workspace, ContainerID: cid, ImageID: expectedImage, Image: namespace + "/container/codex-superpowers:2026.09.30-8e8751383862", Network: "bridge", Utilities: []string{"rtk"}, SecurityPolicy: containerpolicy.ID, SecurityPolicyDigest: containerpolicy.SHA256}
	return operations.Target{Category: config.CategoryContainer, Workspace: workspace, Profile: entry.Key(), Entry: entry}
}

func TestDaemonCleanupRejectsUntrustedIdentityBeforeCommand(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("daemon acceptance helper is confined to macOS /private/tmp")
	}
	// No CLI can run: identity failures must happen before command dispatch.
	t.Setenv("PATH", "")
	for _, fixture := range []struct {
		name     string
		pid, uid int
		server   string
	}{{"wrong-user", 123, os.Getuid() + 1, "fixture-server"}, {"missing-server", 123, os.Getuid(), ""}, {"invalid-pid", 0, os.Getuid(), "fixture-server"}} {
		t.Run(fixture.name, func(t *testing.T) {
			root, err := os.MkdirTemp("/private/tmp", "paseo-sbx-userns-live-fixture-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(root) })
			home := filepath.Join(root, "paseo")
			if err := os.Mkdir(home, 0700); err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(map[string]any{"pid": fixture.pid, "uid": fixture.uid, "serverId": fixture.server})
			path := filepath.Join(home, "paseo.pid")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			err = cleanupDaemon(context.Background(), home)
			if err == nil || !strings.Contains(err.Error(), "identity mismatch") {
				t.Fatalf("expected rejection before CLI, got %v", err)
			}
			actual, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(data, actual) {
				t.Fatalf("identity rejection changed PID record: %v", err)
			}
		})
	}
}

func TestDaemonCleanupAlreadyStopped(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("daemon acceptance helper is confined to macOS /private/tmp")
	}
	t.Setenv("PATH", "")
	root, err := os.MkdirTemp("/private/tmp", "paseo-sbx-userns-live-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	if err := cleanupDaemon(context.Background(), filepath.Join(root, "paseo")); err != nil {
		t.Fatal(err)
	}
}

func TestDaemonCleanupRejectsUnrelatedHome(t *testing.T) {
	if err := cleanupDaemon(context.Background(), filepath.Join(t.TempDir(), "paseo")); err == nil || !strings.Contains(err.Error(), "canonical isolated acceptance home") {
		t.Fatalf("unrelated home accepted: %v", err)
	}
}
