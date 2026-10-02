package operations

import (
	"paseo-sbx/internal/config"
	"time"
)

type Kind string

const (
	KindBuild      Kind = "build"
	KindLoad       Kind = "load"
	KindCreate     Kind = "create"
	KindUpdate     Kind = "update"
	KindStop       Kind = "stop"
	KindRegister   Kind = "register"
	KindUnregister Kind = "unregister"
	KindPrune      Kind = "prune"
	KindVersions   Kind = "versions"
)

type Target struct {
	Workspace, Runtime, Profile string
	Entry                       config.Entry
}
type Request struct {
	Kind   Kind
	Target Target
	// For register/unregister a nonnil Targets slice (including empty) is
	// the confirmed complete active scope; nil preserves legacy CLI resolution.
	Targets []Target
	// RestoreTargets pins the desired completed saved-selection entries while
	// Targets continues to pin the prior active scope.
	RestoreTargets, PruneTargets                                   []Target
	Selection                                                      config.Selection
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
