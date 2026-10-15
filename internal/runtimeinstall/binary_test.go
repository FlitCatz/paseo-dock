package runtimeinstall

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestBinaryAgentChecksumAndNotices(t *testing.T) {
	files := map[string]string{"/omp-linux-arm64": "#!binary", "/LICENSE": "MIT License", "/NOTICES": "third party"}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if body, ok := files[r.URL.Path]; ok {
			w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	sum := func(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
	for _, kind := range []string{"valid", "checksum", "notice", "existing"} {
		t.Run(kind, func(t *testing.T) {
			binarySum, noticeSum := sum(files["/omp-linux-arm64"]), sum(files["/NOTICES"])
			if kind == "checksum" {
				binarySum = sum("other")
			}
			if kind == "notice" {
				noticeSum = sum("other")
			}
			artifact := map[string]any{"integration": "binary-v1", "assets": map[string]any{"aarch64": map[string]string{"url": server.URL + "/omp-linux-arm64", "sha256": binarySum}}, "notices": []map[string]string{{"name": "LICENSE", "url": server.URL + "/LICENSE", "sha256": sum(files["/LICENSE"])}, {"name": "THIRD-PARTY-NOTICES.txt", "url": server.URL + "/NOTICES", "sha256": noticeSum}}}
			b, _ := json.Marshal(map[string]any{"agent_artifacts": map[string]any{"omp": artifact}})
			dir := t.TempDir()
			lock := filepath.Join(dir, "lock.json")
			os.WriteFile(lock, b, 0600)
			dest, notices := filepath.Join(dir, "bin", "omp"), filepath.Join(dir, "licenses", "omp")
			// A fresh image has no destination directory yet.
			if kind == "existing" {
				os.MkdirAll(filepath.Dir(dest), 0755)
				os.WriteFile(dest, []byte("old"), 0755)
			}
			err := installBinaryAgent(server.Client(), lock, dest, notices, "arm64", "omp")
			if kind != "valid" {
				if err == nil {
					t.Fatal("accepted", kind)
				}
				if _, statErr := os.Stat(notices); kind != "existing" && statErr == nil {
					t.Fatal("partial install left notices")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(dest)
			license, _ := os.ReadFile(filepath.Join(notices, "LICENSE"))
			if err != nil || info.Mode().Perm() != 0755 || string(license) != "MIT License" {
				t.Fatal(info, err, string(license))
			}
			if _, err := os.Stat(filepath.Join(notices, "THIRD-PARTY-NOTICES.txt")); err != nil {
				t.Fatal(err)
			}
		})
	}
	if err := InstallBinaryAgent(filepath.Join(t.TempDir(), "missing.json"), "/x", "/y", "arm64", "omp"); err == nil {
		t.Fatal("missing lock accepted")
	}
}

func TestPinnedBinaryAgentsInLock(t *testing.T) {
	var lock struct {
		Artifacts map[string]binaryArtifact `json:"agent_artifacts"`
	}
	b, err := os.ReadFile("../../versions.lock.json")
	if err != nil || json.Unmarshal(b, &lock) != nil {
		t.Fatal(err)
	}
	for _, agent := range []string{"omp", "gjc"} {
		a := lock.Artifacts[agent]
		if a.Integration != "binary-v1" || len(a.Assets) != 2 || len(a.Notices) != 2 || a.Notices[0].Name != "LICENSE" {
			t.Fatal(agent, a)
		}
		for _, asset := range a.Assets {
			if len(asset.SHA256) != 64 {
				t.Fatal(agent, asset)
			}
		}
	}
}
