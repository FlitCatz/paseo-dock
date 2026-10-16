package gateway

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDockerRelayKeepsHostCredentialOutOfGuest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer HOST_SECRET" {
			t.Error("host credential absent upstream")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	}))
	defer upstream.Close()
	calls := 0
	relay, err := NewDockerRelay([]string{"container-codex-superpowers"}, func(endpoint string) error {
		calls++
		res, err := http.Get(strings.Replace(endpoint, "host.docker.internal", "127.0.0.1", 1))
		if err != nil {
			return err
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if string(b) != "paseo-dock-relay-ready" {
			return errors.New("unexpected challenge response")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	endpoint, headers, err := relay.Bind(upstream.URL+"/mcp/agents?callerAgentId=parent", map[string]string{"Authorization": "Bearer HOST_SECRET"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || strings.Contains(endpoint+headers["Authorization"], "HOST_SECRET") {
		t.Fatal("guest credential leak or missing reachability probe")
	}
	req, _ := http.NewRequest("POST", strings.Replace(endpoint, "host.docker.internal", "127.0.0.1", 1), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	req.Header.Set("Authorization", headers["Authorization"])
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	relay.Close()
	if _, _, err := relay.Bind(upstream.URL+"/mcp/agents?callerAgentId=parent", nil); err == nil {
		t.Fatal("closed relay issued capability")
	}
}
