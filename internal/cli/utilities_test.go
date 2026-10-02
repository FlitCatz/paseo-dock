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
	for _, agent := range []string{"codex", "claude", "antigravity-cli"} {
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
			os.WriteFile(filepath.Join(toolSource, "rtk"), []byte("#!/bin/sh\nprintf 'rtk %s\\n' \"$*\" >> \"$COMMAND_LOG\"\nif [ \"$1 $2 $3\" = 'init --agent antigravity' ]; then mkdir -p .agents/rules; printf '# RTK test instructions\\nUse rtk commands.\\n' > .agents/rules/antigravity-rtk-rules.md; fi\n"), 0700)
			os.WriteFile(filepath.Join(toolSource, "uv"), []byte("#!/bin/sh\nprintf 'uv %s\\n' \"$*\" >> \"$COMMAND_LOG\"\nif [ \"$1\" = venv ]; then for last; do :; done; mkdir -p \"$last/bin\"; printf '#!/bin/sh\\necho headroom\\n' > \"$last/bin/headroom\"; chmod +x \"$last/bin/headroom\"; fi\n"), 0700)
			for _, name := range []string{"codex", "claude"} {
				os.WriteFile(filepath.Join(fake, name), []byte("#!/bin/sh\nprintf '"+name+" %s\\n' \"$*\" >> \"$COMMAND_LOG\"\n"), 0700)
			}
			utilities := "rtk,headroom"
			if agent == "antigravity-cli" {
				utilities = "rtk"
				os.MkdirAll(filepath.Join(home, ".gemini"), 0755)
				os.WriteFile(filepath.Join(home, ".gemini", "GEMINI.md"), []byte("Existing sandbox instructions\n"), 0600)
			}
			cmd := exec.Command("bash", script, agent, utilities, lock)
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
			} else if agent == "claude" {
				if !strings.Contains(text, "rtk init -g --auto-patch") || !strings.Contains(text, "claude mcp add --transport stdio --scope user headroom -- ") {
					t.Fatal(text)
				}
			} else {
				if !strings.Contains(text, "rtk init --agent antigravity") || strings.Contains(text, "rtk init -g") || strings.Contains(text, "mcp add") {
					t.Fatal(text)
				}
				instructions, err := os.ReadFile(filepath.Join(home, ".gemini", "GEMINI.md"))
				if err != nil || !strings.Contains(string(instructions), "Existing sandbox instructions") || !strings.Contains(string(instructions), "Use rtk commands.") {
					t.Fatalf("global instructions: %s %v", instructions, err)
				}
			}
			if agent != "antigravity-cli" && (!strings.Contains(text, "headroom-ai[mcp]==0.39.1") || !strings.Contains(text, "mcp serve")) {
				t.Fatal(text)
			}
			tools := []string{"rtk"}
			if agent != "antigravity-cli" {
				tools = append(tools, "uv", "headroom")
			}
			for _, n := range tools {
				if _, err := os.Stat(filepath.Join(bin, n)); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestUnconfiguredAntigravityHeadroomFailsBeforeInstall(t *testing.T) {
	home := t.TempDir()
	cmd := exec.Command("bash", "../../scripts/install-utilities.sh", "antigravity-cli", "rtk,headroom", "../../versions.lock.json")
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "Headroom integration for antigravity-cli is not configured") {
		t.Fatalf("%v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".local")); !os.IsNotExist(err) {
		t.Fatal("partial utility installation")
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
