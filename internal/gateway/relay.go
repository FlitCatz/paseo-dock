package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type BindFunc func(string, map[string]string) (string, map[string]string, error)
type Runner func([]string, bool) error
type route struct {
	url     string
	headers map[string]string
	key     string
	policy  *Policy
}
type Relay struct {
	providers       []string
	sandbox         string
	run             Runner
	mu              sync.Mutex
	routes          map[string]route
	server          *http.Server
	client          *http.Client
	port            int
	allowed, closed bool
	closeOnce       sync.Once
	cancel          context.CancelFunc
	checkDocker     func(string) error
	challenge       string
}

const RelayReady = "paseo-sbx-relay-ready"

// DockerProbeCommand ignores guest curl configuration and proxy environment.
func DockerProbeCommand(endpoint string) []string {
	return []string{"curl", "-q", "--noproxy", "*", "--fail", "--silent", "--show-error", "--max-time", "5", endpoint}
}

// NewDockerRelay keeps the host loopback listener. The caller must prove that
// this exact listener is reachable from the inspected guest before any route.
func NewDockerRelay(providers []string, check func(string) error) (*Relay, error) {
	if check == nil {
		return nil, errors.New("Docker relay requires a guest reachability probe")
	}
	return newRelay(providers, "", nil, check)
}

func (r *Relay) CheckReachability() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.allowLocked()
}
func (r *Relay) allowLocked() error {
	if r.closed {
		return errors.New("relay closed")
	}
	if r.allowed {
		return nil
	}
	if r.checkDocker != nil {
		endpoint := fmt.Sprintf("http://host.docker.internal:%d/%s", r.port, r.challenge)
		if err := r.checkDocker(endpoint); err != nil {
			return fmt.Errorf("Docker bridge cannot reach local Paseo relay (curl required; container must be running): %w", err)
		}
	} else {
		if r.run == nil {
			return errors.New("relay transport unavailable")
		}
		if err := r.run([]string{"policy", "allow", "network", "--sandbox", r.sandbox, fmt.Sprintf("localhost:%d", r.port)}, true); err != nil {
			return errors.New("cannot allow sandbox relay port")
		}
	}
	r.allowed = true
	return nil
}

func NewRelay(providers []string, sandbox string, run Runner) (*Relay, error) {
	return newRelay(providers, sandbox, run, nil)
}
func newRelay(providers []string, sandbox string, run Runner, check func(string) error) (*Relay, error) {
	challenge := ""
	if check != nil {
		var err error
		challenge, err = token()
		if err != nil {
			return nil, err
		}
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	r := &Relay{providers: providers, sandbox: sandbox, run: run, routes: map[string]route{}, port: ln.Addr().(*net.TCPAddr).Port, checkDocker: check, challenge: challenge}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	r.client = &http.Client{Timeout: 180 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.server = &http.Server{Handler: http.HandlerFunc(r.handle), ReadHeaderTimeout: 10 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	go r.server.Serve(ln)
	return r, nil
}
func token() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func (r *Relay) Bind(endpoint string, headers map[string]string) (string, map[string]string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", nil, errors.New("invalid MCP endpoint")
	}
	host := u.Hostname()
	caller := u.Query().Get("callerAgentId")
	if (host != "127.0.0.1" && host != "localhost" && host != "::1") || u.Path != "/mcp/agents" || caller == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", nil, errors.New("only local, agent-scoped Paseo MCP endpoints may be relayed")
	}
	id, err := token()
	if err != nil {
		return "", nil, err
	}
	key, err := token()
	if err != nil {
		return "", nil, err
	}
	upstream := map[string]string{}
	for k, v := range headers {
		upstream[k] = v
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return "", nil, errors.New("relay closed")
	}
	if err := r.allowLocked(); err != nil {
		return "", nil, err
	}
	r.routes[id] = route{url: endpoint, headers: upstream, key: key, policy: NewPolicy(r.providers, caller)}
	return fmt.Sprintf("http://host.docker.internal:%d/%s", r.port, id), map[string]string{"Authorization": "Bearer " + key}, nil
}
func (r *Relay) Close() {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		r.routes = map[string]route{}
		allowed := r.allowed
		r.mu.Unlock()
		r.cancel()
		r.server.Close()
		r.client.CloseIdleConnections()
		if allowed && r.checkDocker == nil && r.run != nil {
			r.run([]string{"policy", "rm", "network", "--sandbox", r.sandbox, "--resource", fmt.Sprintf("localhost:%d", r.port), "--force"}, false)
		}
	})
}
func reject(w http.ResponseWriter, id any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(400)
	json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32602, "message": "Request rejected by sandbox delegation policy"}})
}
func (r *Relay) handle(w http.ResponseWriter, req *http.Request) {
	// A random, credential-free challenge proves reachability of this listener.
	// It never forwards traffic or reveals the upstream endpoint or credentials.
	if r.challenge != "" && req.URL.Path == "/"+r.challenge && req.Method == "GET" {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, RelayReady)
		return
	}
	if req.Method != "POST" && req.Method != "DELETE" {
		http.Error(w, "method not allowed", 405)
		return
	}
	r.mu.Lock()
	route, ok := r.routes[strings.TrimPrefix(req.URL.Path, "/")]
	r.mu.Unlock()
	if !ok || subtle.ConstantTimeCompare([]byte(req.Header.Get("Authorization")), []byte("Bearer "+route.key)) != 1 {
		http.Error(w, "forbidden", 403)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, 4*1024*1024))
	if err != nil {
		http.Error(w, "request exceeds relay limit", 413)
		return
	}
	operation := ""
	var id any
	if req.Method == "POST" {
		var value any
		if err = json.Unmarshal(body, &value); err != nil {
			reject(w, nil)
			return
		}
		if m, ok := value.(map[string]any); ok {
			id = m["id"]
		}
		if err = route.policy.Check(value); err != nil {
			reject(w, id)
			return
		}
		m := value.(map[string]any)
		operation, _ = m["method"].(string)
		if params, ok := m["params"].(map[string]any); ok {
			if name, ok := params["name"].(string); ok {
				operation = name
			}
		}
	}
	upstream, err := http.NewRequestWithContext(req.Context(), req.Method, route.url, bytes.NewReader(body))
	if err != nil {
		reject(w, id)
		return
	}
	for _, key := range []string{"Content-Type", "Accept", "Mcp-Session-Id", "Mcp-Protocol-Version", "Last-Event-Id"} {
		if v := req.Header.Get(key); v != "" {
			upstream.Header.Set(key, v)
		}
	}
	for k, v := range route.headers {
		upstream.Header.Set(k, v)
	}
	res, err := r.client.Do(upstream)
	if err != nil {
		http.Error(w, "Paseo MCP upstream unavailable", 502)
		return
	}
	defer res.Body.Close()
	response, err := io.ReadAll(io.LimitReader(res.Body, 8*1024*1024+1))
	if err != nil {
		http.Error(w, "Paseo MCP upstream unavailable", 502)
		return
	}
	if len(response) > 8*1024*1024 {
		reject(w, id)
		return
	}
	contentType := res.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		response, err = RewriteResponse(response, contentType, route.policy, operation)
		if err != nil {
			reject(w, id)
			return
		}
	}
	response, err = redactResponse(response, contentType, route.headers)
	if err != nil {
		reject(w, id)
		return
	}
	for _, key := range []string{"Content-Type", "Mcp-Session-Id", "MCP-Protocol-Version"} {
		if v := res.Header.Get(key); v != "" {
			w.Header().Set(key, redactString(v, route.headers))
		}
	}
	w.Header().Set("Content-Length", fmt.Sprint(len(response)))
	w.WriteHeader(res.StatusCode)
	w.Write(response)
}

func redactString(value string, headers map[string]string) string {
	for _, credential := range headers {
		for _, secret := range []string{credential, strings.TrimPrefix(credential, "Bearer ")} {
			if secret == "" {
				continue
			}
			encoded, _ := json.Marshal(secret)
			value = strings.ReplaceAll(value, string(encoded[1:len(encoded)-1]), "[redacted]")
			value = strings.ReplaceAll(value, secret, "[redacted]")
		}
	}
	return value
}
func redactResponse(body []byte, contentType string, headers map[string]string) ([]byte, error) {
	var walk func(any) any
	walk = func(value any) any {
		switch v := value.(type) {
		case string:
			return redactString(v, headers)
		case map[string]any:
			r := map[string]any{}
			for k, x := range v {
				r[redactString(k, headers)] = walk(x)
			}
			return r
		case []any:
			r := make([]any, len(v))
			for i, x := range v {
				r[i] = walk(x)
			}
			return r
		default:
			return value
		}
	}
	result, err := rewriteMessages(body, contentType, func(value any) (any, error) { return walk(value), nil })
	if err != nil && !strings.Contains(contentType, "text/event-stream") && !strings.Contains(contentType, "json") {
		return []byte(redactString(string(body), headers)), nil
	}
	return result, err
}
