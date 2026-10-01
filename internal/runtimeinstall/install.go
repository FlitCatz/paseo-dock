// Package runtimeinstall installs checksum-pinned official CLI distributions.
package runtimeinstall

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// InstallAgent installs the native Antigravity CLI; it does not install an ACP server.
func InstallAgent(lockPath, destination, machine, agent string) error {
	if agent != "antigravity-cli" {
		return fmt.Errorf("unsupported binary agent %q", agent)
	}
	key := map[string]string{"arm64": "aarch64", "aarch64": "aarch64", "amd64": "x86_64", "x86_64": "x86_64"}[machine]
	if key == "" {
		return fmt.Errorf("unsupported architecture %q", machine)
	}
	var lock struct {
		Artifacts map[string]struct {
			Assets map[string]struct {
				URL    string `json:"url"`
				SHA512 string `json:"sha512"`
			} `json:"assets"`
		} `json:"agent_artifacts"`
	}
	b, err := os.ReadFile(lockPath)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &lock); err != nil {
		return err
	}
	asset := lock.Artifacts[agent].Assets[key]
	expected, err := hex.DecodeString(asset.SHA512)
	if err != nil || len(expected) != sha512.Size || asset.URL == "" {
		return errors.New("missing or invalid CLI artifact")
	}
	if _, err = os.Lstat(destination); err == nil {
		return errors.New("CLI destination already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(filepath.Dir(destination), ".cli-install-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	client := &http.Client{Timeout: 5 * time.Minute}
	res, err := client.Get(asset.URL)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("CLI download returned HTTP %d", res.StatusCode)
	}
	f, err := os.Create(filepath.Join(dir, "cli.tar.gz"))
	if err != nil {
		return err
	}
	h := sha512.New()
	const limit = 512 * 1024 * 1024
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(res.Body, limit+1))
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n > limit {
		return errors.New("CLI archive exceeds limit")
	}
	if hex.EncodeToString(h.Sum(nil)) != hex.EncodeToString(expected) {
		return errors.New("official CLI archive checksum mismatch")
	}
	archive, err := os.Open(filepath.Join(dir, "cli.tar.gz"))
	if err != nil {
		return err
	}
	defer archive.Close()
	z, err := gzip.NewReader(archive)
	if err != nil {
		return err
	}
	defer z.Close()
	r := tar.NewReader(z)
	found := false
	for {
		header, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if strings.TrimPrefix(header.Name, "./") != "antigravity" {
			continue
		}
		if found || header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > limit {
			return errors.New("invalid CLI binary in archive")
		}
		f, err := os.OpenFile(filepath.Join(dir, "agy"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(f, r)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		found = true
	}
	if !found {
		return errors.New("CLI archive does not contain antigravity")
	}
	return os.Rename(filepath.Join(dir, "agy"), destination)
}

func Install(lockPath, destination, machine string) error {
	return install(lockPath, destination, machine, "https://nodejs.org/dist")
}
func install(lockPath, destination, machine, baseURL string) error {
	var lock struct {
		Node struct {
			Version string            `json:"version"`
			SHA256  map[string]string `json:"sha256"`
		} `json:"node"`
	}
	b, err := os.ReadFile(lockPath)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &lock); err != nil {
		return err
	}
	arch, key := "", ""
	switch machine {
	case "arm64", "aarch64":
		arch, key = "arm64", "aarch64"
	case "amd64", "x86_64":
		arch, key = "x64", "x86_64"
	default:
		return fmt.Errorf("unsupported architecture %q", machine)
	}
	if lock.Node.Version == "" || strings.ContainsAny(lock.Node.Version, "/\\") {
		return errors.New("invalid Node version")
	}
	expected, err := hex.DecodeString(lock.Node.SHA256[key])
	if err != nil || len(expected) != sha256.Size {
		return errors.New("missing or invalid Node checksum")
	}
	if _, err = os.Lstat(destination); err == nil {
		return errors.New("Node destination already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(destination)
	if err = os.MkdirAll(parent, 0755); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(parent, ".node-install-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	name := "node-" + lock.Node.Version + "-linux-" + arch
	client := &http.Client{Timeout: 5 * time.Minute}
	res, err := client.Get(strings.TrimRight(baseURL, "/") + "/" + lock.Node.Version + "/" + name + ".tar.xz")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("Node download returned HTTP %d", res.StatusCode)
	}
	archive := filepath.Join(dir, "node.tar.xz")
	file, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	const maxArchive = 256 * 1024 * 1024
	n, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(res.Body, maxArchive+1))
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if n > maxArchive {
		return errors.New("Node archive exceeds limit")
	}
	if hex.EncodeToString(hash.Sum(nil)) != hex.EncodeToString(expected) {
		return errors.New("official Node archive checksum mismatch")
	}
	listing, err := exec.Command("tar", "-tJf", archive).Output()
	if err != nil {
		return errors.New("cannot inspect Node archive")
	}
	for _, line := range strings.Split(strings.TrimSpace(string(listing)), "\n") {
		clean := filepath.Clean(line)
		if clean != name && !strings.HasPrefix(clean, name+"/") {
			return errors.New("Node archive contains unexpected paths")
		}
	}
	extraction := filepath.Join(dir, "extracted")
	if err = os.Mkdir(extraction, 0755); err != nil {
		return err
	}
	if output, err := exec.Command("tar", "-xJf", archive, "-C", extraction).CombinedOutput(); err != nil {
		return fmt.Errorf("cannot extract Node archive: %s", strings.TrimSpace(string(output)))
	}
	source := filepath.Join(extraction, name)
	info, err := os.Stat(filepath.Join(source, "bin", "node"))
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("Node archive does not contain bin/node")
	}
	return os.Rename(source, destination)
}
