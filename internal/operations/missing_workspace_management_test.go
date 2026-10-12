package operations

import (
	"context"
	"os"
	"testing"

	"paseo-sbx/internal/config"
)

func TestStopAndRemoveMissingWorkspaceKeepRegistryReachable(t *testing.T) {
	for _, category := range []config.Category{config.CategorySandbox, config.CategoryContainer} {
		t.Run(string(category)+"/stop", func(t *testing.T) {
			s, target, backend := categoryFixture(t, category)
			if err := os.RemoveAll(target.Workspace); err != nil {
				t.Fatal(err)
			}
			result, err := s.Execute(context.Background(), Request{Kind: KindStop, Target: target}, nil)
			if err != nil || !result.Changed || len(backend.calls) != 1 || backend.calls[0] != "stop:"+target.Profile {
				t.Fatalf("missing-workspace stop result=%+v err=%v calls=%v", result, err, backend.calls)
			}
		})
		t.Run(string(category)+"/remove", func(t *testing.T) {
			s, request, path, backend := removalFixture(t)
			if category == config.CategoryContainer {
				request.Target.Entry.Category = category
				request.Target.Entry.ContainerID = "confirmed-container-id"
				request.Target.Entry.Sandbox = ""
				request.Target.Category = category
				request.Target.Profile = request.Target.Entry.Key()
				writeTarget(t, request.Target)
				s.Backends = map[config.Category]Backend{category: backend}
				backend.state.ID = request.Target.Entry.ContainerID
			}
			if err := os.RemoveAll(request.Target.Workspace); err != nil {
				t.Fatal(err)
			}
			result, err := s.Execute(context.Background(), request, nil)
			if err != nil || !result.Changed || backend.removed != 1 {
				t.Fatalf("missing-workspace remove result=%+v err=%v removed=%d registry=%s", result, err, backend.removed, path)
			}
		})
	}
}
