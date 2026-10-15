package operations

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"paseo-sbx/internal/config"
	"strings"
	"time"
)

const acpProbeTimeout = 10 * time.Second

// acpInitialize is the first request of every ACP session.
const acpInitialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{}}}` + "\n"

// checkACP runs each ACP-connected agent's pinned server in the container and
// requires a valid initialize response before the entry is activated.
func (a *execution) checkACP(backend Backend, profile string, entry config.Entry) error {
	for _, t := range ProviderTargets(profile, entry) {
		if t.Mode != config.ConnectionACP {
			continue
		}
		argv := config.ACPCommand(t.Agent)
		if len(argv) == 0 {
			return fmt.Errorf("no ACP server is pinned for %s", t.Agent)
		}
		ctx, cancel := context.WithTimeout(a.ctx, acpProbeTimeout)
		var out bytes.Buffer
		// Closing stdin after the request ends the session; a server that keeps
		// running is stopped by the timeout and judged on what it wrote.
		execErr := backend.Exec(ctx, entry.ForAgent(t.Agent), argv, strings.NewReader(acpInitialize), &out, io.Discard)
		cancel()
		if !acpInitialized(out.Bytes()) {
			return fmt.Errorf("ACP check failed for %s: no valid initialize response (%v); choose another connection for it", t.Agent, execErr)
		}
	}
	return nil
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
