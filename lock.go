// Package versionlock embeds the same release lock consumed by image builds.
package versionlock

import (
	_ "embed"
	"encoding/json"

	"paseo-dock/internal/config"
)

//go:embed versions.lock.json
var data []byte

// Load returns an independent copy of the lock shipped with this executable.
func Load() config.Lock {
	var lock config.Lock
	if err := json.Unmarshal(data, &lock); err != nil {
		panic(err) // The checked-in build input must be valid JSON.
	}
	return lock
}
