package operations

import (
	"paseo-dock/internal/config"
	"time"
)

type Kind string

const (
	KindBuild      Kind = "build"
	KindLoad       Kind = "load"
	KindCreate     Kind = "create"
	KindUpdate     Kind = "update"
	KindStop       Kind = "stop"
	KindRemove     Kind = "remove"
	KindRegister   Kind = "register"
	KindUnregister Kind = "unregister"
	KindPrune      Kind = "prune"
	KindVersions   Kind = "versions"
)

type Target struct {
	Category                    config.Category
	Workspace, Runtime, Profile string
	Entry                       config.Entry
}

func (t Target) EqualIdentity(other Target) bool {
	return t.Workspace == other.Workspace && t.Runtime == other.Runtime && t.Profile == other.Profile && t.Category == other.Category && t.Entry.EqualIdentity(other.Entry)
}

// ResourceIdentity exposes the backend identity even if registry persistence failed.
func (t Target) ResourceIdentity() string {
	if t.Entry.EffectiveCategory() == config.CategoryContainer {
		return "container/" + t.Entry.ContainerID
	}
	return t.Entry.Sandbox
}

type Request struct {
	Kind   Kind
	Target Target
	// AutomaticUpdate preserves opt-out and retry intervals for CLI shell entry.
	// Explicit CLI/TUI updates leave this false and force a refresh.
	AutomaticUpdate bool
	RemoveRuntimes  bool
	// For register/unregister a nonnil Targets slice (including empty) is
	// the confirmed complete active scope; nil preserves legacy CLI resolution.
	Targets []Target
	// RestoreTargets pins the desired completed saved-selection entries while
	// Targets continues to pin the prior active scope.
	RestoreTargets, PruneTargets                                   []Target
	Selection                                                      config.Selection
	Repository                                                     config.Repository
	Ports                                                          []config.PortMapping
	Namespace, Platforms, Memory, PaseoHome                        string
	Push, KeepArchive, BuildFirst, Apply, DryRun, RestoreSelection bool
}
type Event struct {
	Stage, Text string
	Time        time.Time
}
type Result struct {
	Changed, NeedsVerification bool
	Versions                   map[string]string
	VersionCheckedAt           time.Time
	VersionErrors              map[string]string
	Warnings                   []string
	Remaining, Missing         []Target
}
