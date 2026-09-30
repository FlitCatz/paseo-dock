// Package gateway enforces session-scoped delegation capabilities.
package gateway

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
)

var toolArguments = map[string][]string{
	"create_agent":      {"provider", "initialPrompt", "title", "notifyOnFinish", "settings", "labels"},
	"send_agent_prompt": {"agentId", "prompt", "background", "notifyOnFinish"},
	"get_agent_status":  {"agentId"}, "list_agents": {"limit", "statuses", "includeArchived", "sinceHours"},
	"cancel_agent": {"agentId"}, "get_agent_activity": {"agentId", "limit"}, "list_providers": {}, "list_models": {"provider"},
}

// errDropMessage removes one event from an SSE response; elsewhere it rejects.
var errDropMessage = errors.New("upstream message dropped by sandbox delegation policy")

type Policy struct {
	providers map[string]bool
	agents    map[string]bool
	mu        sync.Mutex
}

func NewPolicy(providers []string, caller string) *Policy {
	p := &Policy{providers: map[string]bool{}, agents: map[string]bool{caller: true}}
	for _, v := range providers {
		p.providers[v] = true
	}
	return p
}
func allowedKey(keys []string, key string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}
func (p *Policy) Check(value any) error {
	rejected := errors.New("request rejected by sandbox delegation policy")
	req, ok := value.(map[string]any)
	if !ok || req == nil {
		return rejected
	}
	method, _ := req["method"].(string)
	switch method {
	case "initialize", "notifications/initialized", "notifications/cancelled", "ping", "tools/list":
		return nil
	case "tools/call":
	default:
		return rejected
	}
	params, ok := req["params"].(map[string]any)
	if !ok {
		return rejected
	}
	name, _ := params["name"].(string)
	keys, ok := toolArguments[name]
	if !ok {
		return rejected
	}
	args := map[string]any{}
	if v, exists := params["arguments"]; exists {
		args, ok = v.(map[string]any)
		if !ok || args == nil {
			return rejected
		}
	}
	for k := range args {
		if !allowedKey(keys, k) {
			return rejected
		}
	}
	if name == "create_agent" || name == "list_models" {
		provider, ok := args["provider"].(string)
		base, _, _ := strings.Cut(provider, "/")
		if !ok || !p.providers[base] {
			return rejected
		}
	}
	if allowedKey(keys, "agentId") {
		id, ok := args["agentId"].(string)
		if !ok {
			return rejected
		}
		p.mu.Lock()
		found := p.agents[id]
		p.mu.Unlock()
		if !found {
			return rejected
		}
	}
	if name == "create_agent" {
		if v, exists := args["settings"]; exists {
			settings, ok := v.(map[string]any)
			if !ok || settings == nil {
				return rejected
			}
			for k := range settings {
				if k != "modeId" && k != "thinkingOptionId" {
					return rejected
				}
			}
		}
	}
	return nil
}
func clone(value any) any {
	switch v := value.(type) {
	case map[string]any:
		r := map[string]any{}
		for k, x := range v {
			r[k] = clone(x)
		}
		return r
	case []any:
		r := make([]any, len(v))
		for i, x := range v {
			r[i] = clone(x)
		}
		return r
	default:
		return value
	}
}
func sanitize(value any) any {
	switch v := value.(type) {
	case map[string]any:
		r := map[string]any{}
		for k, x := range v {
			if !allowedKey([]string{"config", "mcpServers", "mcp_servers", "env", "providerOptions", "headers", "http_headers"}, k) {
				r[k] = sanitize(x)
			}
		}
		return r
	case []any:
		r := make([]any, len(v))
		for i, x := range v {
			r[i] = sanitize(x)
		}
		return r
	default:
		return value
	}
}
func (p *Policy) Filter(value any, operation string) (any, error) {
	original, ok := value.(map[string]any)
	if !ok || original == nil {
		return nil, errors.New("unexpected upstream response")
	}
	// Server notifications and requests on the response stream are not needed
	// by the sandbox and may describe other agents; drop them instead of failing.
	if _, isMessage := original["method"]; isMessage {
		return nil, errDropMessage
	}
	response := clone(original).(map[string]any)
	result, ok := response["result"].(map[string]any)
	if !ok {
		_, toolCall := toolArguments[operation]
		if toolCall || operation == "tools/list" {
			if _, hasResult := response["result"]; !hasResult {
				if upstreamError, valid := response["error"].(map[string]any); valid {
					_, messageValid := upstreamError["message"].(string)
					codeValid := false
					switch code := upstreamError["code"].(type) {
					case int:
						codeValid = true
					case float64:
						codeValid = code == float64(int64(code))
					}
					if messageValid && codeValid {
						return map[string]any{"jsonrpc": "2.0", "id": response["id"], "error": map[string]any{"code": upstreamError["code"], "message": "Paseo MCP request failed"}}, nil
					}
				}
			}
			return nil, errors.New("invalid upstream tool result; refusing unfiltered response")
		}
		return response, nil
	}
	if operation == "create_agent" && result["isError"] != true {
		if data, ok := result["structuredContent"].(map[string]any); ok {
			if id, ok := data["agentId"].(string); ok && id != "" {
				p.mu.Lock()
				p.agents[id] = true
				p.mu.Unlock()
			}
		}
	}
	if operation == "tools/list" {
		tools, _ := result["tools"].([]any)
		filtered := []any{}
		for _, v := range tools {
			tool, ok := v.(map[string]any)
			if !ok {
				return nil, errors.New("invalid tool catalog")
			}
			name, _ := tool["name"].(string)
			keys, ok := toolArguments[name]
			if !ok {
				continue
			}
			if schema, ok := tool["inputSchema"].(map[string]any); ok {
				if properties, ok := schema["properties"].(map[string]any); ok {
					for key := range properties {
						if !allowedKey(keys, key) {
							delete(properties, key)
						}
					}
				}
				if required, ok := schema["required"].([]any); ok {
					kept := []any{}
					for _, key := range required {
						if s, ok := key.(string); ok && allowedKey(keys, s) {
							kept = append(kept, key)
						}
					}
					schema["required"] = kept
				}
			}
			filtered = append(filtered, tool)
		}
		result["tools"] = filtered
		delete(result, "nextCursor")
		return response, nil
	}
	raw, exists := result["structuredContent"]
	if !exists {
		_, toolCall := toolArguments[operation]
		if toolCall && result["isError"] == true {
			// Like JSON-RPC errors, report the failure without upstream details.
			response["result"] = map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": "Paseo MCP tool call failed"}}}
			return response, nil
		}
		if toolCall {
			return nil, errors.New("missing structured upstream result; refusing unfiltered response")
		}
		return response, nil
	}
	data, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("unexpected upstream result; refusing unfiltered response")
	}
	if operation == "list_providers" || operation == "list_agents" {
		key := "providers"
		if operation == "list_agents" {
			key = "agents"
		}
		items, _ := data[key].([]any)
		filtered := []any{}
		p.mu.Lock()
		for _, v := range items {
			item, ok := v.(map[string]any)
			if !ok {
				continue
			}
			id, _ := item["id"].(string)
			if id == "" {
				id, _ = item["agentId"].(string)
			}
			allowed := p.providers[id]
			if key == "agents" {
				allowed = p.agents[id]
			}
			if allowed {
				filtered = append(filtered, item)
			}
		}
		p.mu.Unlock()
		data[key] = filtered
	}
	data = sanitize(data).(map[string]any)
	result["structuredContent"] = data
	b, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	result["content"] = []any{map[string]any{"type": "text", "text": string(b)}}
	return response, nil
}
func RewriteResponse(body []byte, contentType string, p *Policy, operation string) ([]byte, error) {
	return rewriteMessages(body, contentType, func(value any) (any, error) { return p.Filter(value, operation) })
}
func rewriteMessages(body []byte, contentType string, transform func(any) (any, error)) ([]byte, error) {
	if len(body) == 0 {
		return body, nil
	}
	rewrite := func(data []byte) ([]byte, error) {
		var v any
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		out, err := transform(v)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	}
	if !strings.Contains(contentType, "text/event-stream") {
		return rewrite(body)
	}
	events := strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n\n")
	rewritten := []string{}
	for _, event := range events {
		if event == "" {
			continue
		}
		lines := strings.Split(event, "\n")
		data := []string{}
		kept := []string{}
		for _, line := range lines {
			if strings.HasPrefix(line, "data:") {
				data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			} else {
				kept = append(kept, line)
			}
		}
		if len(data) > 0 {
			b, err := rewrite([]byte(strings.Join(data, "\n")))
			if errors.Is(err, errDropMessage) {
				continue
			}
			if err != nil {
				return nil, err
			}
			kept = append(kept, "data: "+string(b))
		}
		rewritten = append(rewritten, strings.Join(kept, "\n"))
	}
	return []byte(strings.Join(rewritten, "\n\n") + "\n\n"), nil
}
