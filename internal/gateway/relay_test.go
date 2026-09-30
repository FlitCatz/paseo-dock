package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestHTTPRelay(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer HOST_SECRET_123456" {
			t.Error("upstream credential missing")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"structuredContent":{"snapshot":{"id":"parent","status":"idle","config":{"secret":"HOST_SECRET_123456"}}},"content":[{"text":"HOST_SECRET_123456"}]}}`)
	}))
	defer upstream.Close()
	var mu sync.Mutex
	var calls [][]string
	relay, err := NewRelay([]string{"codex-superpowers"}, "box", func(args []string, check bool) error {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, append([]string{}, args...))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	u, h, err := relay.Bind(upstream.URL+"/mcp/agents?callerAgentId=parent", map[string]string{"Authorization": "Bearer HOST_SECRET_123456"})
	if err != nil {
		t.Fatal(err)
	}
	local := strings.Replace(u, "host.docker.internal", "127.0.0.1", 1)
	for _, tc := range []struct {
		body   string
		auth   string
		status int
	}{
		{`{"method":"tools/call","params":{"name":"get_agent_status","arguments":{"agentId":"parent"}}}`, h["Authorization"], 200},
		{`{"method":"tools/call","params":{"name":"create_terminal","arguments":{}}}`, h["Authorization"], 400},
		{`[]`, h["Authorization"], 400}, {`{`, h["Authorization"], 400}, {`{}`, "Bearer wrong", 403},
	} {
		req, _ := http.NewRequest("POST", local, strings.NewReader(tc.body))
		req.Header.Set("Authorization", tc.auth)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != tc.status {
			t.Errorf("status %d, want %d: %s", res.StatusCode, tc.status, b)
		}
		if strings.Contains(string(b), "HOST_SECRET") {
			t.Fatalf("credential leak %s", b)
		}
	}
	if _, _, err := relay.Bind("https://example.com/mcp/agents?callerAgentId=parent", nil); err == nil {
		t.Fatal("allowed remote endpoint")
	}
	relay.Close()
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 || calls[0][1] != "allow" || calls[1][1] != "rm" {
		t.Fatalf("policy lifecycle %v", calls)
	}
}
func TestConcurrentRelayBindingsUseOneRule(t *testing.T) {
	var mu sync.Mutex
	allow, remove := 0, 0
	r, err := NewRelay([]string{"codex-superpowers"}, "box", func(args []string, check bool) error {
		mu.Lock()
		defer mu.Unlock()
		if args[1] == "allow" {
			allow++
		} else {
			remove++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := r.Bind("http://127.0.0.1:6767/mcp/agents?callerAgentId=parent", nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	r.Close()
	mu.Lock()
	defer mu.Unlock()
	if allow != 1 || remove != 1 {
		t.Fatalf("allow=%d remove=%d", allow, remove)
	}
}
