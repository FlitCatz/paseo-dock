package operations

import (
	"bytes"
	"fmt"
	"os"
	"paseo-dock/internal/config"
	"paseo-dock/internal/process"
	"path/filepath"
	"strings"
)

func (a *execution) repositoryGit(dir string, args ...string) (string, error) {
	cmd := process.Command(a.ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		if a.ctx.Err() != nil {
			return "", a.ctx.Err()
		}
		return "", fmt.Errorf("repository git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// Called under the workspace operation lock, after selection validation and before build.
// Existing checkouts are inspected only; a failed clone never occupies the destination.
func (a *execution) prepareRepository(workspace string, repo config.Repository) error {
	if repo.URL == "" {
		if repo.Ref != "" {
			return fmt.Errorf("repository.ref requires repository.url")
		}
		return nil
	}
	base, err := config.BaseDir()
	if err != nil {
		return err
	}
	if err = checkWorkspace(base, workspace); err != nil {
		return err
	}
	root, err := config.Canonical(a.Root)
	if err != nil {
		return err
	}
	if config.Within(root, workspace) {
		return fmt.Errorf("workspace must not contain the paseo-dock source root")
	}
	if strings.HasPrefix(repo.URL, "-") {
		return fmt.Errorf("invalid repository URL")
	}
	if repo.Ref != "" {
		if strings.HasPrefix(repo.Ref, "-") {
			return fmt.Errorf("invalid repository ref")
		}
		if _, err = a.repositoryGit("", "check-ref-format", "refs/heads/"+repo.Ref); err != nil {
			return err
		}
	}
	info, err := os.Stat(workspace)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("repository workspace must be a directory")
		}
		top, err := a.repositoryGit(workspace, "rev-parse", "--show-toplevel")
		if err != nil {
			return fmt.Errorf("existing workspace must be a repository checkout: %w", err)
		}
		top, err = config.Canonical(top)
		if err != nil || top != workspace {
			return fmt.Errorf("workspace must be the repository root")
		}
		origin, err := a.repositoryGit(workspace, "remote", "get-url", "origin")
		if err != nil {
			return err
		}
		if origin != repo.URL {
			return fmt.Errorf("existing repository origin differs from repository.url; workspace preserved")
		}
		if repo.Ref != "" {
			head, err := a.repositoryGit(workspace, "rev-parse", "--verify", "HEAD^{commit}")
			if err != nil {
				return err
			}
			ref, err := a.repositoryGit(workspace, "rev-parse", "--verify", "refs/heads/"+repo.Ref+"^{commit}")
			if err != nil {
				ref, err = a.repositoryGit(workspace, "rev-parse", "--verify", "refs/tags/"+repo.Ref+"^{commit}")
			}
			if err != nil || head != ref {
				return fmt.Errorf("existing repository HEAD does not match ref %q; workspace preserved", repo.Ref)
			}
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(workspace)
	if err = os.MkdirAll(parent, 0755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".paseo-dock-clone-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	a.stage("repository", "Cloning repository into "+workspace)
	args := []string{"clone"}
	if repo.Ref != "" {
		args = append(args, "--branch", repo.Ref)
	}
	args = append(args, "--", repo.URL, stage)
	if _, err = a.repositoryGit("", args...); err != nil {
		return err
	}
	if err = a.ctx.Err(); err != nil {
		return err
	}
	if _, err = os.Lstat(workspace); !os.IsNotExist(err) {
		return fmt.Errorf("workspace appeared during clone; refusing to replace it")
	}
	return os.Rename(stage, workspace)
}
