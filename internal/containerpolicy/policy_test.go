package containerpolicy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestProfile(t *testing.T) {
	b, err := Profile()
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(b)) != SHA256 {
		t.Fatal("digest")
	}
	b[0] = 0
	fresh, _ := Profile()
	if fresh[0] == 0 {
		t.Fatal("not a copy")
	}
	var v any
	_ = json.Unmarshal(fresh, &v)
	pretty, _ := json.MarshalIndent(v, "", "  ")
	if !MatchesJSON(pretty) || MatchesJSON([]byte(`{}`)) {
		t.Fatal("semantic match")
	}
	if MatchesJSON(append(fresh, []byte(`{}`)...)) {
		t.Fatal("trailing JSON accepted")
	}
	path, cleanup, err := WriteTemp()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("permissions")
	}
	data, _ := os.ReadFile(path)
	if !MatchesJSON(data) {
		t.Fatal("temp content")
	}
	cleanup()
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("cleanup")
	}
}
