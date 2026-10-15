package runtimeinstall

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// binaryArtifact is a single-file release binary pinned by sha256, with the
// license and notice files that must ship alongside it.
type binaryArtifact struct {
	Integration string `json:"integration"`
	Assets      map[string]struct {
		URL    string `json:"url"`
		SHA256 string `json:"sha256"`
	} `json:"assets"`
	Notices []struct {
		Name   string `json:"name"`
		URL    string `json:"url"`
		SHA256 string `json:"sha256"`
	} `json:"notices"`
}

// InstallBinaryAgent installs a checksum-pinned single-binary agent CLI (omp,
// gjc) and its license/notice files.
func InstallBinaryAgent(lockPath, destination, notices, machine, agent string) error {
	return installBinaryAgent(&http.Client{Timeout: 10 * time.Minute}, lockPath, destination, notices, machine, agent)
}

func installBinaryAgent(client *http.Client, lockPath, destination, notices, machine, agent string) error {
	key := map[string]string{"arm64": "aarch64", "aarch64": "aarch64", "amd64": "x86_64", "x86_64": "x86_64"}[machine]
	if key == "" {
		return fmt.Errorf("unsupported architecture %q", machine)
	}
	var lock struct {
		Artifacts map[string]json.RawMessage `json:"agent_artifacts"`
	}
	b, err := os.ReadFile(lockPath)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &lock); err != nil {
		return err
	}
	var artifact binaryArtifact
	if raw, ok := lock.Artifacts[agent]; !ok || json.Unmarshal(raw, &artifact) != nil || artifact.Integration != "binary-v1" {
		return fmt.Errorf("unsupported binary agent %q", agent)
	}
	if len(artifact.Notices) == 0 || notices == "" {
		return errors.New("binary agent requires license and notice files")
	}
	for _, path := range []string{destination, notices} {
		if _, err = os.Lstat(path); err == nil {
			return fmt.Errorf("destination already exists: %s", path)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	asset := artifact.Assets[key]
	stage, err := os.MkdirTemp(filepath.Dir(destination), ".cli-install-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	binary := filepath.Join(stage, "cli")
	if err = download(client, asset.URL, asset.SHA256, binary, 512*1024*1024); err != nil {
		return fmt.Errorf("%s binary: %w", agent, err)
	}
	if err = os.Chmod(binary, 0755); err != nil {
		return err
	}
	docs := filepath.Join(stage, "notices")
	if err = os.Mkdir(docs, 0755); err != nil {
		return err
	}
	for _, n := range artifact.Notices {
		if n.Name == "" || n.Name != filepath.Base(n.Name) || strings.HasPrefix(n.Name, ".") {
			return fmt.Errorf("invalid notice name %q", n.Name)
		}
		if err = download(client, n.URL, n.SHA256, filepath.Join(docs, n.Name), 16*1024*1024); err != nil {
			return fmt.Errorf("%s %s: %w", agent, n.Name, err)
		}
		if err = os.Chmod(filepath.Join(docs, n.Name), 0644); err != nil {
			return err
		}
	}
	if err = os.MkdirAll(filepath.Dir(notices), 0755); err != nil {
		return err
	}
	if err = os.Rename(docs, notices); err != nil {
		return err
	}
	return os.Rename(binary, destination)
}

// download fetches an https URL into path, enforcing a size limit and sha256.
func download(client *http.Client, url, sum, path string, limit int64) error {
	expected, err := hex.DecodeString(sum)
	if err != nil || len(expected) != sha256.Size || !strings.HasPrefix(url, "https://") {
		return errors.New("missing or invalid pinned artifact")
	}
	res, err := client.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d", res.StatusCode)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(res.Body, limit+1))
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n > limit {
		return errors.New("download exceeds limit")
	}
	if hex.EncodeToString(h.Sum(nil)) != sum {
		return errors.New("checksum mismatch")
	}
	return nil
}
