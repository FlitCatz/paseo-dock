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
	Kind                                                           Kind
	Target                                                         Target
	Targets                                                        []Target
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
	Remaining, Missing         []Target
}
