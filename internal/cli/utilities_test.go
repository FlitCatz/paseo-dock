package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Installer subprocesses are replaced because this contract test must not
// download runtimes or change the developer's real agent configuration.
func TestUtilityInstallerIntegrations(t *testing.T) {
	script, _ := filepath.Abs("../../scripts/install-utilities.sh")
	lock, _ := filepath.Abs("../../versions.lock.json")
	for _, agent := range []string{"codex", "claude"} {
		t.Run(agent, func(t *testing.T) {
			home := t.TempDir()
			bin := filepath.Join(home, ".local", "bin")
			os.MkdirAll(bin, 0755)
			fake := t.TempDir()
			log := filepath.Join(home, "commands")
			os.WriteFile(filepath.Join(fake, "curl"), []byte("#!/bin/sh\nwhile [ $# -gt 0 ]; do if [ \"$1\" = -o ]; then touch \"$2\"; exit; fi; shift; done\n"), 0700)
			os.WriteFile(filepath.Join(fake, "sha256sum"), []byte("#!/bin/sh\ncat >/dev/null\n"), 0700)
			os.WriteFile(filepath.Join(fake, "tar"), []byte("#!/bin/sh\nwhile [ $# -gt 0 ]; do if [ \"$1\" = -C ]; then d=$2; break; fi; shift; done\nmkdir -p \"$d/tools\"\ncp \"$FAKE_TOOLS\"/* \"$d/tools/\"\n"), 0700)
			toolSource := t.TempDir()
			os.WriteFile(filepath.Join(toolSource, "rtk"), []byte("#!/bin/sh\nprintf 'rtk %s\\n' \"$*\" >> \"$COMMAND_LOG\"\n"), 0700)
			os.WriteFile(filepath.Join(toolSource, "uv"), []byte("#!/bin/sh\nprintf 'uv %s\\n' \"$*\" >> \"$COMMAND_LOG\"\nif [ \"$1\" = venv ]; then for last; do :; done; mkdir -p \"$last/bin\"; printf '#!/bin/sh\\necho headroom\\n' > \"$last/bin/headroom\"; chmod +x \"$last/bin/headroom\"; fi\n"), 0700)
			for _, name := range []string{"codex", "claude"} {
				os.WriteFile(filepath.Join(fake, name), []byte("#!/bin/sh\nprintf '"+name+" %s\\n' \"$*\" >> \"$COMMAND_LOG\"\n"), 0700)
			}
			cmd := exec.Command("bash", script, agent, "rtk,headroom", lock)
			cmd.Env = append(os.Environ(), "HOME="+home, "PATH="+fake+":"+bin+":"+os.Getenv("PATH"), "COMMAND_LOG="+log, "FAKE_TOOLS="+toolSource)
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v %s", err, b)
			}
			b, _ := os.ReadFile(log)
			text := string(b)
			if agent == "codex" {
				if !strings.Contains(text, "rtk init -g --codex") || !strings.Contains(text, "codex mcp add headroom -- ") {
					t.Fatal(text)
				}
			} else {
				if !strings.Contains(text, "rtk init -g --auto-patch") || !strings.Contains(text, "claude mcp add --transport stdio --scope user headroom -- ") {
					t.Fatal(text)
				}
			}
			if !strings.Contains(text, "headroom-ai[mcp]==0.39.1") || !strings.Contains(text, "mcp serve") {
				t.Fatal(text)
			}
			for _, n := range []string{"rtk", "uv", "headroom"} {
				if _, err := os.Stat(filepath.Join(bin, n)); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
func TestUnknownUtilityFailsBeforeInstall(t *testing.T) {
	cmd := exec.Command("bash", "../../scripts/install-utilities.sh", "codex", "rtk,unknown", "../../versions.lock.json")
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "unsupported utility") {
		t.Fatalf("%v %s", err, out)
	}
}
