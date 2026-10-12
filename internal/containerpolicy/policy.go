// Package containerpolicy bundles the reviewed, fixed Docker Codex seccomp policy.
package containerpolicy

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
)

const ID = "codex-userns-v1"
const SHA256 = "de699bf56093ed501d6cf021db43b48f6bd5bd843a715404e7d0714ff0ed0af8"

//go:embed codex-userns-v1.json
var profile []byte

func Profile() ([]byte, error) {
	if fmt.Sprintf("%x", sha256.Sum256(profile)) != SHA256 {
		return nil, fmt.Errorf("embedded seccomp profile digest mismatch")
	}
	return bytes.Clone(profile), nil
}

// MatchesJSON accepts formatting differences but no changes to the policy.
func MatchesJSON(observed []byte) bool {
	expected, err := Profile()
	if err != nil {
		return false
	}
	var a, b any
	da := json.NewDecoder(bytes.NewReader(expected))
	da.UseNumber()
	db := json.NewDecoder(bytes.NewReader(observed))
	db.UseNumber()
	if !json.Valid(observed) || da.Decode(&a) != nil || db.Decode(&b) != nil {
		return false
	}
	return reflect.DeepEqual(a, b)
}

// WriteTemp returns a private profile file and an idempotent cleanup function.
func WriteTemp() (string, func(), error) {
	b, err := Profile()
	if err != nil {
		return "", nil, err
	}
	f, err := os.CreateTemp("", "paseo-sbx-seccomp-*.json")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	if _, err = f.Write(b); err != nil {
		_ = f.Close()
		cleanup()
		return "", nil, err
	}
	if err = f.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return f.Name(), cleanup, nil
}
