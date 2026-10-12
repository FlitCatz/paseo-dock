package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operations"
	"path/filepath"
	"strings"
	"testing"
)

type startupNoticeBackend struct {
	cliCategoryBackend
	errout *bytes.Buffer
	before []string
}

func (b *startupNoticeBackend) Exec(context.Context, config.Entry, []string, io.Reader, io.Writer, io.Writer) error {
	b.before = append(b.before, b.errout.String())
	return errors.New("probe stopped at exec marker")
}
func (b *startupNoticeBackend) Inspect(context.Context, config.Entry) (operations.RuntimeState, error) {
	return operations.RuntimeState{Known: true, Running: false}, nil
}

func TestCLIStartupNoticePrecedesGuestExecution(t *testing.T) {
	for _, category := range []config.Category{config.CategorySandbox, config.CategoryLegacySandbox, config.CategoryContainer} {
		for _, command := range []string{"shell", "update", "versions"} {
			t.Run(string(category)+"/"+command, func(t *testing.T) {
				w, runtime := signalFixture(t)
				var reg config.Registry
				if err := config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg); err != nil {
					t.Fatal(err)
				}
				e := reg.Profiles["codex-superpowers"]
				if category != config.CategoryLegacySandbox {
					e.Category = category
				}
				if category == config.CategorySandbox {
					e.Tool = ""
				}
				if category == config.CategoryContainer {
					e.ContainerID = strings.Repeat("a", 64)
					e.ImageID = "sha256:managed"
					e.Network = "bridge"
				}
				key := e.Key()
				reg.Profiles = map[string]config.Entry{key: e}
				reg.Active = []string{key}
				if err := config.WriteJSON(filepath.Join(runtime, "registry.json"), reg); err != nil {
					t.Fatal(err)
				}
				a, err := NewApp("../..")
				if err != nil {
					t.Fatal(err)
				}
				var stdout, stderr bytes.Buffer
				a.Out = &stdout
				a.Err = &stderr
				b := &startupNoticeBackend{errout: &stderr}
				a.Backends = map[config.Category]operations.Backend{category: b}
				_ = a.execute([]string{command, key, "--workspace", w})
				if len(b.before) == 0 {
					t.Fatal("guest exec marker was not reached")
				}
				for _, before := range b.before {
					notice := strings.Contains(before, "may start a stopped sandbox")
					if category == config.CategoryContainer {
						if notice {
							t.Fatal("Docker falsely announced SBX automatic startup")
						}
					} else if !notice {
						t.Fatal("startup notice missing before guest exec", before)
					}
				}
				if command == "versions" {
					if !json.Valid(stdout.Bytes()) {
						t.Fatal("warning contaminated versions JSON", stdout.String())
					}
				} else if strings.Contains(stdout.String(), "may start a stopped sandbox") {
					t.Fatal("warning contaminated stdout protocol")
				}
			})
		}
	}
}
