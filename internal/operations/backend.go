package operations

import (
	"context"
	"io"
	"paseo-sbx/internal/config"
)

// RuntimeState is observed runtime data. Known is false when inspection fails;
// callers must not interpret that state as an absent resource.
type RuntimeState struct {
	ID      string
	ImageID string
	Running bool
	Known   bool
	Mounts  []RuntimeMount
	Network string
	Ports   []config.PortMapping
}
type RuntimeMount struct {
	Type, Source, Destination string
	RW                        bool
}
type Backend interface {
	List(context.Context) ([]RuntimeState, error)
	Create(context.Context, config.Entry) (config.Entry, error)
	Inspect(context.Context, config.Entry) (RuntimeState, error)
	Exec(context.Context, config.Entry, []string, io.Reader, io.Writer, io.Writer) error
	Stop(context.Context, config.Entry) error
}
