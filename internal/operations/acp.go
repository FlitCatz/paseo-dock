package operations

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"paseo-dock/internal/config"
	"slices"
	"strings"
	"time"
)

const acpProbeTimeout = 10 * time.Second

// acpInitialize is the first request of every ACP session.
const acpInitialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{}}}` + "\n"

// checkACP runs each ACP-connected agent's pinned server in the container and
// returns the agents that gave no valid initialize response.
func (a *execution) checkACP(backend Backend, profile string, entry config.Entry) map[string]error {
	failed := map[string]error{}
	for _, t := range ProviderTargets(profile, entry) {
		if t.Mode != config.ConnectionACP {
			continue
		}
		argv := config.ACPCommand(t.Agent)
		if len(argv) == 0 {
			failed[t.Agent] = fmt.Errorf("no ACP server is pinned for %s", t.Agent)
			continue
		}
		ctx, cancel := context.WithTimeout(a.ctx, acpProbeTimeout)
		var out bytes.Buffer
		// Closing stdin after the request ends the session; a server that keeps
		// running is stopped by the timeout and judged on what it wrote.
		execErr := backend.Exec(ctx, entry.ForAgent(t.Agent), argv, strings.NewReader(acpInitialize), &out, io.Discard)
		cancel()
		if !acpInitialized(out.Bytes()) {
			failed[t.Agent] = fmt.Errorf("no valid ACP initialize response (%v)", execErr)
		}
	}
	return failed
}

// recordACP checks one entry and records its failed ACP providers as disabled
// in the registry; passing providers are re-enabled. Only the failed agents
// lose their Paseo provider, and the returned error names each of them.
func (a *execution) recordACP(backend Backend, reg *config.Registry, profile string, entry config.Entry) error {
	failed := a.checkACP(backend, profile, entry)
	var errs []error
	for _, t := range ProviderTargets(profile, entry) {
		disabled := slices.Contains(reg.DisabledProviders, t.ID)
		err, failedNow := failed[t.Agent]
		switch {
		case t.Mode == config.ConnectionACP && failedNow:
			if !disabled {
				reg.DisabledProviders = append(reg.DisabledProviders, t.ID)
			}
			errs = append(errs, fmt.Errorf("ACP check failed for %s in %s; its Paseo provider %s stays disabled until a later register passes: %w", t.Agent, profile, t.ID, err))
		case disabled:
			reg.DisabledProviders = slices.DeleteFunc(reg.DisabledProviders, func(id string) bool { return id == t.ID })
		}
	}
	slices.Sort(reg.DisabledProviders)
	if len(reg.DisabledProviders) == 0 {
		reg.DisabledProviders = nil
	}
	return errors.Join(errs...)
}

func acpInitialized(output []byte) bool {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var response struct {
			ID     json.RawMessage `json:"id"`
			Result *struct {
				ProtocolVersion int `json:"protocolVersion"`
			} `json:"result"`
		}
		if json.Unmarshal(scanner.Bytes(), &response) == nil && string(response.ID) == "1" && response.Result != nil && response.Result.ProtocolVersion > 0 {
			return true
		}
	}
	return false
}
