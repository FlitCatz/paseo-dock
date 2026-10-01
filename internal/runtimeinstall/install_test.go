package runtimeinstall

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeAgentChecksumAndArchive(t *testing.T) {
	for _, kind := range []string{"valid", "checksum", "symlink", "missing"} {
		t.Run(kind, func(t *testing.T) {
			var payload bytes.Buffer
			gz := gzip.NewWriter(&payload)
			archive := tar.NewWriter(gz)
			h := &tar.Header{Name: "antigravity", Mode: 0755, Size: 6, Typeflag: tar.TypeReg}
			if kind == "symlink" {
				h.Typeflag = tar.TypeSymlink
				h.Size = 0
				h.Linkname = "/etc/passwd"
			}
			if kind == "missing" {
				h.Name = "other"
			}
			archive.WriteHeader(h)
			if h.Size > 0 {
				archive.Write([]byte("binary"))
			}
			archive.Close()
			gz.Close()
			sum := sha512.Sum512(payload.Bytes())
			checksum := hex.EncodeToString(sum[:])
			if kind == "checksum" {
				checksum = strings.Repeat("0", 128)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(payload.Bytes()) }))
			defer server.Close()
			dir := t.TempDir()
			lock, _ := json.Marshal(map[string]any{"agent_artifacts": map[string]any{"antigravity-cli": map[string]any{"assets": map[string]any{"aarch64": map[string]string{"url": server.URL, "sha512": checksum}}}}})
			path := filepath.Join(dir, "lock.json")
			os.WriteFile(path, lock, 0600)
			dest := filepath.Join(dir, "bin", "agy")
			err := InstallAgent(path, dest, "arm64", "antigravity-cli")
			if kind == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				b, _ := os.ReadFile(dest)
				if string(b) != "binary" {
					t.Fatal(string(b))
				}
			} else {
				if err == nil {
					t.Fatal("invalid artifact accepted")
				}
				if _, err := os.Stat(dest); !os.IsNotExist(err) {
					t.Fatal("invalid binary installed")
				}
			}
		})
	}
}

func TestVerifiedInstall(t *testing.T) {
	dir := t.TempDir()
	name := "node-v24.21.0-linux-arm64"
	os.MkdirAll(filepath.Join(dir, name, "bin"), 0755)
	os.WriteFile(filepath.Join(dir, name, "bin", "node"), []byte("#!/bin/sh\necho node\n"), 0755)
	archive := filepath.Join(dir, "node.tar.xz")
	if b, err := exec.Command("tar", "-cJf", archive, "-C", dir, name).CombinedOutput(); err != nil {
		t.Fatalf("tar %v %s", err, b)
	}
	payload, _ := os.ReadFile(archive)
	sum := sha256.Sum256(payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v24.21.0/node-v24.21.0-linux-arm64.tar.xz" {
			t.Error(r.URL.Path)
		}
		w.Write(payload)
	}))
	defer server.Close()
	lock := filepath.Join(dir, "lock.json")
	writeLock := func(checksum string) {
		b, _ := json.Marshal(map[string]any{"node": map[string]any{"version": "v24.21.0", "sha256": map[string]string{"aarch64": checksum}}})
		os.WriteFile(lock, b, 0600)
	}
	writeLock(hex.EncodeToString(sum[:]))
	dest := filepath.Join(dir, "installed")
	if err := install(lock, dest, "arm64", server.URL); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "bin", "node")); err != nil {
		t.Fatal(err)
	}
	writeLock("0000000000000000000000000000000000000000000000000000000000000000")
	bad := filepath.Join(dir, "bad")
	if err := install(lock, bad, "aarch64", server.URL); err == nil {
		t.Fatal("accepted wrong checksum")
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatal("created destination before verifying checksum")
	}
	if err := install(lock, bad, "unknown", server.URL); err == nil {
		t.Fatal("accepted architecture")
	}
}
func TestFailedDownloadDoesNotInstall(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	dir := t.TempDir()
	lock := filepath.Join(dir, "lock.json")
	os.WriteFile(lock, []byte(`{"node":{"version":"v24.21.0","sha256":{"x86_64":"0000000000000000000000000000000000000000000000000000000000000000"}}}`), 0600)
	dest := filepath.Join(dir, "node")
	if err := install(lock, dest, "amd64", server.URL); err == nil {
		t.Fatal("accepted HTTP 404")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("installed failed download")
	}
}
