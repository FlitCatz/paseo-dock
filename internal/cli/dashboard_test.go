package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestTUIRequiresTerminal(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
		text string
	}{
		{[]string{"tui"}, 1, "terminal"},
		{nil, 2, "usage:"},
		{[]string{"tui", "--help"}, 0, "workspace"},
		{[]string{"--help"}, 0, "usage:"},
	} {
		var out, stderr bytes.Buffer
		got := Run(t.TempDir(), tc.args, strings.NewReader(""), &out, &stderr)
		if got != tc.code || !strings.Contains(out.String()+stderr.String(), tc.text) {
			t.Errorf("args %v: code %d, output %q; want %d containing %q", tc.args, got, out.String()+stderr.String(), tc.code, tc.text)
		}
	}
}
