package operations

import (
	"context"
	"os"
	"os/exec"
	"paseo-sbx/internal/config"
	"path/filepath"
	"strings"
	"testing"
)

func repositoryService(t *testing.T) (*Service, *categoryBackend) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	lock, err := os.ReadFile("../../versions.lock.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "versions.lock.json"), lock, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(root, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "bin", "agent-wrapper"), []byte("wrapper"), 0700); err != nil {
		t.Fatal(err)
	}
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	b := &categoryBackend{}
	s.Backends = map[config.Category]Backend{config.CategoryContainer: b}
	return s, b
}

func repoGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestCreatePreparesRepositoryAndPreservesExistingWork(t *testing.T) {
	s, b := repositoryService(t)
	origin := t.TempDir()
	repoGit(t, origin, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(origin, "README.md"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	repoGit(t, origin, "add", ".")
	repoGit(t, origin, "commit", "-m", "initial")
	workspace := filepath.Join(t.TempDir(), "checkout")
	request := Request{Kind: KindCreate, Target: Target{Workspace: workspace}, Repository: config.Repository{URL: origin, Ref: "main"}, Selection: config.Selection{Schema: 2, Category: config.CategoryContainer, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}}}
	_, err := s.Execute(context.Background(), request, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(workspace, "README.md"))
	if err != nil || string(data) != "original" {
		t.Fatalf("repository not cloned: %q %v", data, err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("local edits"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = s.Execute(context.Background(), request, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(workspace, "README.md"))
	if string(data) != "local edits" {
		t.Fatal("existing work overwritten")
	}
	b.calls = nil
	request.Repository.URL = t.TempDir()
	_, err = s.Execute(context.Background(), request, nil)
	if err == nil || !strings.Contains(err.Error(), "origin") || len(b.calls) != 0 {
		t.Fatalf("wrong origin accepted: %v %v", err, b.calls)
	}
	request.Repository.URL = origin
	request.Repository.Ref = "other-branch"
	_, err = s.Execute(context.Background(), request, nil)
	if err == nil || len(b.calls) != 0 {
		t.Fatalf("wrong ref accepted: %v %v", err, b.calls)
	}
}

func TestCreateRepositoryFailureDoesNotLeaveWorkspace(t *testing.T) {
	s, b := repositoryService(t)
	workspace := filepath.Join(t.TempDir(), "checkout")
	_, err := s.Execute(context.Background(), Request{Kind: KindCreate, Target: Target{Workspace: workspace}, Repository: config.Repository{URL: filepath.Join(t.TempDir(), "missing")}, Selection: config.Selection{Schema: 2, Category: config.CategoryContainer, Pairs: []config.Pair{{Agent: "codex", Methodology: "superpowers"}}}}, nil)
	if err == nil || len(b.calls) != 0 {
		t.Fatalf("clone failure reached container: %v %v", err, b.calls)
	}
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatalf("failed clone left destination: %v", err)
	}
}
