// Package nodeinstall installs a checksum-pinned official Node distribution.
package nodeinstall

import (
	"crypto/sha256"
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
