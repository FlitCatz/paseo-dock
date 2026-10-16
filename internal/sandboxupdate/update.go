// Package sandboxupdate refreshes software in existing sandboxes without replacing them.
// All installation commands run in the guest; host code never executes guest files.
package sandboxupdate

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"paseo-dock/internal/config"
	"paseo-dock/internal/operationlock"
	"paseo-dock/internal/process"
)

type State struct {
	Sandbox  string            `json:"sandbox"`
	Image    string            `json:"image,omitempty"`
	Attempt  time.Time         `json:"attempt"`
	Success  time.Time         `json:"success,omitempty"`
	Versions map[string]string `json:"versions,omitempty"`
	Errors   map[string]string `json:"errors,omitempty"`
}

type runner func(context.Context, []string, bool) (string, error)

// Transport executes guest commands without exposing host package tooling.
type Transport func(context.Context, []string, bool) (string, error)

func Identity(entry config.Entry) string {
	if entry.EffectiveCategory() == config.CategoryContainer {
		if entry.ContainerID == "" {
			return ""
		}
		return "container/" + entry.ContainerID
	}
	return entry.Sandbox
}

func UpdateContext(ctx context.Context, runtime string, entry config.Entry, force bool, log io.Writer, emit func(string)) error {
	if entry.Sandbox == "" || !supported(entry) {
		return errors.New("invalid sandbox entry")
	}
	if !force && os.Getenv("PASEO_DOCK_AUTO_UPDATE") == "0" {
		return nil
	}
	release, err := operationlock.Acquire(ctx, runtime, []string{operationlock.EntryKey(entry)})
	if err != nil {
		if !force && errors.Is(err, operationlock.ErrConflict) {
			return nil
		}
		return err
	}
	defer release()
	return UpdateLockedContext(ctx, runtime, entry, force, log, emit)
}

// Update checks at most daily (hourly after failures). Force bypasses the interval.
// Failures are returned to explicit callers; automatic callers can continue launching.
func Update(runtime string, entry config.Entry, force bool, log io.Writer) error {
	return UpdateContext(context.Background(), runtime, entry, force, log, nil)
}

// UpdateLockedContext requires the caller to hold runtime's matching SandboxKey
// lock. It avoids reacquisition when the caller validates a frozen target under lock.
func UpdateLockedContext(ctx context.Context, runtime string, entry config.Entry, force bool, log io.Writer, emit func(string)) error {
	return UpdateTransportLockedContext(ctx, runtime, entry, force, log, emit, nil)
}

// UpdateTransportLockedContext requires the matching EntryKey lock held by caller.
func UpdateTransportLockedContext(ctx context.Context, runtime string, entry config.Entry, force bool, log io.Writer, emit func(string), transport Transport) error {
	if entry.EffectiveCategory() == config.CategoryContainer && transport == nil {
		return errors.New("container update requires an explicit guest transport")
	}
	if Identity(entry) == "" || !supported(entry) {
		return errors.New("invalid sandbox entry")
	}
	if !force && os.Getenv("PASEO_DOCK_AUTO_UPDATE") == "0" {
		return nil
	}
	sum := sha256.Sum256([]byte(Identity(entry)))
	dir := filepath.Join(runtime, "updates")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	base := filepath.Join(dir, fmt.Sprintf("%x", sum[:8]))
	var prior State
	_ = config.ReadJSON(base+".json", &prior)
	interval := 24 * time.Hour
	if len(prior.Errors) > 0 {
		interval = time.Hour
	}
	if !force && prior.Sandbox == Identity(entry) && prior.Image == entry.Image && time.Since(prior.Attempt) < interval {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	run := func(ctx context.Context, args []string, capture bool) (string, error) {
		// A fixed guest directory prevents workspace npm configuration from affecting installs.
		command := []string{"exec"}
		if entry.EffectiveCategory() == config.CategorySandbox {
			command = append(command, "--env", "PATH="+config.SandboxAgentPath)
		}
		command = append(command, "--workdir", "/tmp", entry.Sandbox)
		cmd := process.Command(ctx, "sbx", append(command, args...)...)
		cmd.Stderr = log
		if capture {
			b, err := cmd.Output()
			return strings.TrimSpace(string(b)), err
		}
		cmd.Stdout = log
		return "", cmd.Run()
	}
	fmt.Fprintln(log, "paseo-dock: updating", Identity(entry))
	if transport != nil {
		run = transport
	}
	state := refreshStages(ctx, entry, run, time.Now(), emit)
	return finishUpdate(ctx, base, state, log)
}

// finishUpdate persists the attempt before reporting its completion status.
func finishUpdate(ctx context.Context, base string, state State, log io.Writer) error {
	if err := config.WriteJSON(base+".json", state); err != nil {
		return errors.Join(err, ctx.Err())
	}
	if len(state.Errors) > 0 {
		if ctx.Err() != nil {
			return fmt.Errorf("sandbox update incomplete; guest state needs verification: %w", ctx.Err())
		}
		return fmt.Errorf("sandbox update incomplete: %v", state.Errors)
	}
	fmt.Fprintln(log, "paseo-dock: updated", state.Sandbox, state.Versions)
	return nil
}

type updater struct {
	ctx        context.Context
	run        runner
	home, root string
}

func (u updater) command(args ...string) error          { _, err := u.run(u.ctx, args, false); return err }
func (u updater) output(args ...string) (string, error) { return u.run(u.ctx, args, true) }

func refresh(ctx context.Context, entry config.Entry, run runner, now time.Time) State {
	return refreshStages(ctx, entry, run, now, nil)
}
func refreshStages(ctx context.Context, entry config.Entry, run runner, now time.Time, emit func(string)) State {
	s := State{Sandbox: Identity(entry), Image: entry.Image, Attempt: now, Versions: map[string]string{}, Errors: map[string]string{}}
	if emit != nil {
		emit("environment")
	}
	u := updater{ctx: ctx, run: run}
	home, err := u.output("printenv", "HOME")
	if err != nil || !strings.HasPrefix(home, "/") || strings.ContainsAny(home, "\r\n") {
		s.Errors["environment"] = "cannot determine guest home"
		return s
	}
	u.home = home
	u.root = home + "/.local/share/paseo-dock/updates"
	if err := u.command("mkdir", "-p", u.root, home+"/.local/bin"); err != nil {
		s.Errors["environment"] = err.Error()
		return s
	}
	step := func(id string, fn func() (string, error)) {
		if emit != nil {
			emit(id)
		}
		if err := ctx.Err(); err != nil {
			s.Errors[id] = err.Error()
			return
		}
		v, err := fn()
		if err != nil {
			s.Errors[id] = err.Error()
		} else {
			s.Versions[id] = v
		}
	}
	// A bundle refreshes every agent and methodology under its own stage key.
	agents := []string{entry.Agent}
	if entry.Bundle() {
		agents = entry.AgentIDs()
	}
	for _, agent := range agents {
		id := "agent"
		if entry.Bundle() {
			id = "agent:" + agent
		}
		switch agent {
		case "codex":
			step(id, func() (string, error) { return u.npm("@openai/codex", "codex") })
		case "claude":
			step(id, func() (string, error) { return u.npm("@anthropic-ai/claude-code", "claude") })
		case "antigravity-cli":
			step(id, u.antigravity)
		case "omp", "gjc":
			// Pinned by the image lock; their self-updaters are not used.
		default:
			s.Errors[id] = "unsupported agent adapter"
		}
	}
	if entry.EffectiveCategory() == config.CategorySandbox {
		if len(s.Errors) == 0 {
			s.Success = now
		}
		return s
	}
	// Read the installed lock from the guest, including custom package and utility IDs.
	raw, err := u.output("cat", home+"/.local/share/paseo-dock/installed.json")
	var installed struct {
		Lock config.Lock `json:"lock"`
	}
	if err != nil || json.Unmarshal([]byte(raw), &installed) != nil {
		s.Errors["methodology"] = "cannot read guest installation manifest"
	} else if entry.Bundle() {
		// Each methodology updates once for all agents paired with it.
		var tools []string
		agentsByTool := map[string][]string{}
		for _, p := range entry.Pairs {
			if p.Methodology == "native" {
				continue
			}
			if _, ok := agentsByTool[p.Methodology]; !ok {
				tools = append(tools, p.Methodology)
			}
			agentsByTool[p.Methodology] = append(agentsByTool[p.Methodology], p.Agent)
		}
		slices.Sort(tools)
		for _, tool := range tools {
			step("methodology:"+tool, u.methodology(agentsByTool[tool], tool, installed.Lock))
		}
	} else if entry.Tool != "native" {
		step("methodology", u.methodology([]string{entry.Agent}, entry.Tool, installed.Lock))
	}
	// Absence of a utility manifest means this image has no selected utilities.
	raw, err = u.output("cat", home+"/.local/share/paseo-dock/utilities/installed.json")
	if err == nil {
		var utilities struct {
			Utilities map[string]json.RawMessage `json:"utilities"`
		}
		if err := json.Unmarshal([]byte(raw), &utilities); err != nil {
			s.Errors["utilities"] = err.Error()
		} else {
			for id, raw := range utilities.Utilities {
				var item struct {
					Installer string `json:"installer"`
					Package   string `json:"package"`
				}
				if err := json.Unmarshal(raw, &item); err != nil {
					s.Errors[id] = err.Error()
					continue
				}
				installer := item.Installer
				if installer == "" {
					installer = id
				}
				step(id, func() (string, error) {
					switch installer {
					case "rtk":
						return u.rtk()
					case "headroom":
						return u.headroom(item.Package)
					default:
						return "", fmt.Errorf("unsupported utility %s", installer)
					}
				})
			}
		}
	}
	if len(s.Errors) == 0 {
		s.Success = now
	}
	return s
}

// activate switches a single executable atomically only after verification.
func (u updater) activate(source, binary string) (string, error) {
	v, err := u.output(source, "--version")
	if err != nil {
		return "", fmt.Errorf("verify %s: %w", binary, err)
	}
	target := u.home + "/.local/bin/" + binary
	link, err := u.output("mktemp", "-u", target+".XXXXXXXXXX")
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(link, target+".") || strings.ContainsAny(link, "\r\n") {
		return "", errors.New("invalid activation path")
	}
	if err := u.command("ln", "-s", source, link); err != nil {
		return "", err
	}
	if err := u.command("mv", "-Tf", link, target); err != nil {
		_ = u.command("rm", "-f", link)
		return "", err
	}
	return v, nil
}

func (u updater) stage() (string, error) {
	dir, err := u.output("mktemp", "-d", u.root+"/install.XXXXXXXXXX")
	if err == nil && (!strings.HasPrefix(dir, u.root+"/install.") || strings.ContainsAny(dir, "\r\n")) {
		err = errors.New("invalid staging path")
	}
	return dir, err
}

func (u updater) npm(pkg, binary string) (string, error) {
	if pkg == "" {
		return "", errors.New("missing npm package")
	}
	version, err := u.output("npm", "view", pkg+"@latest", "version")
	if err != nil {
		return "", err
	}
	if version == "" || strings.ContainsAny(version, " \r\n/") {
		return "", errors.New("invalid npm release version")
	}
	if current, err := u.output(binary, "--version"); err == nil {
		for _, field := range strings.Fields(current) {
			if field == version {
				return current, nil
			}
		}
	}
	dir, err := u.stage()
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		if !ok {
			_ = u.command("rm", "-rf", "--", dir)
		}
	}()
	if err = u.command("npm", "install", "--global", "--prefix", dir, "--no-audit", "--no-fund", pkg+"@"+version); err != nil {
		return "", err
	}
	v, err := u.activate(dir+"/bin/"+binary, binary)
	ok = err == nil
	return v, err
}

// plugin fetches a shared plugin checkout once, then reinstalls it for each
// agent; on failure it restores the old revision for every agent touched.
func (u updater) plugin(agents []string, tool string) (string, error) {
	dir := u.home + "/.local/share/paseo-dock/marketplace/plugins/" + tool
	old, err := u.output("git", "-C", dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	dirty, err := u.output("git", "-C", dir, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	if dirty != "" {
		return "", errors.New("plugin has local changes; update skipped")
	}
	if err = u.command("git", "-C", dir, "fetch", "--depth", "1", "origin", "HEAD"); err != nil {
		return "", err
	}
	next, err := u.output("git", "-C", dir, "rev-parse", "FETCH_HEAD")
	if err != nil {
		return "", err
	}
	if next == old {
		return old, nil
	}
	if err = u.command("git", "-C", dir, "checkout", "--detach", "FETCH_HEAD"); err != nil {
		return "", err
	}
	for i, agent := range agents {
		args, err := u.pluginInstall(agent, tool, dir)
		if err == nil && len(args) > 0 {
			err = u.command(args...)
		}
		if err != nil {
			_ = u.command("git", "-C", dir, "checkout", "--detach", old)
			for _, done := range agents[:i+1] {
				// Codex was removed before reinstalling; restore it from the old
				// revision. Other agents re-read the restored checkout.
				if done == "codex" || done != agent {
					if args := u.pluginReinstall(done, tool, dir); len(args) > 0 {
						_ = u.command(args...)
					}
				}
			}
			return "", err
		}
	}
	return u.output("git", "-C", dir, "rev-parse", "HEAD")
}

// pluginInstall prepares an agent for a new plugin revision and returns the
// command that installs it.
func (u updater) pluginInstall(agent, tool, dir string) ([]string, error) {
	if agent == "codex" {
		// Codex has no plugin-update command. Remove its cached copy before
		// reinstalling; renaming a cache directory from an image's lower layer
		// fails with EXDEV on overlay filesystems.
		if err := u.command("codex", "plugin", "remove", tool+"@paseo-dock"); err != nil {
			return nil, err
		}
	}
	return u.pluginReinstall(agent, tool, dir), nil
}

// pluginReinstall returns the command that installs the checked-out revision.
func (u updater) pluginReinstall(agent, tool, dir string) []string {
	switch agent {
	case "codex":
		return []string{"codex", "plugin", "add", tool + "@paseo-dock"}
	case "claude":
		return []string{"claude", "plugin", "update", tool + "@paseo-dock"}
	case "antigravity-cli":
		if tool == "ponytail" {
			return []string{"agy", "plugin", "install", dir}
		}
	}
	return nil
}

func (u updater) antigravity() (string, error) {
	arch, err := u.output("uname", "-m")
	if err != nil {
		return "", err
	}
	platform := "linux_arm64"
	if arch == "x86_64" {
		platform = "linux_amd64"
	} else if arch != "aarch64" && arch != "arm64" {
		return "", fmt.Errorf("unsupported Antigravity architecture %s", arch)
	}
	// This is the release manifest endpoint used by Google's official installer.
	endpoint := "https://antigravity-cli-auto-updater-974169037036.us-central1.run.app/manifests/" + platform + ".json"
	raw, err := u.output("curl", "--fail", "--silent", "--show-error", "--location", "--max-time", "30", "--proto", "=https", endpoint)
	if err != nil {
		return "", err
	}
	var manifest struct {
		Version string `json:"version"`
		URL     string `json:"url"`
		SHA512  string `json:"sha512"`
	}
	if err = json.Unmarshal([]byte(raw), &manifest); err != nil {
		return "", err
	}
	if manifest.Version == "" || !strings.HasPrefix(manifest.URL, "https://storage.googleapis.com/antigravity-public/antigravity-cli/") || len(manifest.SHA512) != 128 {
		return "", errors.New("invalid Antigravity release manifest")
	}
	if current, err := u.output("agy", "--version"); err == nil && current == manifest.Version {
		return current, nil
	}
	dir, err := u.stage()
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		if !ok {
			_ = u.command("rm", "-rf", "--", dir)
		}
	}()
	archive := dir + "/agy.tar.gz"
	if err = u.command("curl", "--fail", "--silent", "--show-error", "--location", "--max-time", "60", "--proto", "=https", "-o", archive, manifest.URL); err != nil {
		return "", err
	}
	digest, err := u.output("sha512sum", archive)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(digest, manifest.SHA512+" ") {
		return "", errors.New("Antigravity checksum mismatch")
	}
	if err = u.command("tar", "-xzf", archive, "-C", dir, "antigravity"); err != nil {
		return "", err
	}
	if err = u.command("chmod", "0755", dir+"/antigravity"); err != nil {
		return "", err
	}
	v, err := u.activate(dir+"/antigravity", "agy")
	ok = err == nil
	return v, err
}

func (u updater) headroom(pkg string) (string, error) {
	if pkg == "" {
		pkg = "headroom-ai[mcp]"
	}
	active, err := u.output("readlink", "-f", u.home+"/.local/bin/headroom")
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(active, u.home+"/.local/") || strings.ContainsAny(active, "\r\n") {
		return "", errors.New("invalid Headroom installation path")
	}
	outdated, err := u.output("uv", "pip", "list", "--python", filepath.Dir(active)+"/python", "--outdated", "--format", "json")
	if err != nil {
		return "", err
	}
	var packages []json.RawMessage
	if err = json.Unmarshal([]byte(outdated), &packages); err != nil {
		return "", err
	}
	if len(packages) == 0 {
		return u.output("headroom", "--version")
	}
	current, err := u.output("uv", "pip", "list", "--python", filepath.Dir(active)+"/python", "--format", "json")
	if err != nil {
		return "", err
	}
	dir, err := u.stage()
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		if !ok {
			_ = u.command("rm", "-rf", "--", dir)
		}
	}()
	python := u.home + "/.local/share/paseo-dock/utilities/headroom/venv/bin/python"
	if err = u.command("uv", "venv", "--python", python, dir+"/venv"); err != nil {
		return "", err
	}
	if err = u.command("uv", "pip", "install", "--python", dir+"/venv/bin/python", "--upgrade", pkg); err != nil {
		return "", err
	}
	resolved, err := u.output("uv", "pip", "list", "--python", dir+"/venv/bin/python", "--format", "json")
	if err != nil {
		return "", err
	}
	// PyPI may list newer dependencies that Headroom's constraints don't permit.
	// Don't retain another identical environment when the resolver picks the same versions.
	if samePackages(current, resolved) {
		return u.output("headroom", "--version")
	}
	v, err := u.activate(dir+"/venv/bin/headroom", "headroom")
	ok = err == nil
	return v, err
}

func samePackages(a, b string) bool {
	parse := func(raw string) (map[string]string, error) {
		var items []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		if err := json.Unmarshal([]byte(raw), &items); err != nil {
			return nil, err
		}
		versions := map[string]string{}
		for _, item := range items {
			versions[item.Name] = item.Version
		}
		return versions, nil
	}
	x, err := parse(a)
	if err != nil {
		return false
	}
	y, err := parse(b)
	return err == nil && maps.Equal(x, y)
}

func (u updater) rtk() (string, error) {
	arch, err := u.output("uname", "-m")
	if err != nil {
		return "", err
	}
	name := "rtk-aarch64-unknown-linux-gnu.tar.gz"
	if arch == "x86_64" {
		name = "rtk-x86_64-unknown-linux-musl.tar.gz"
	} else if arch != "aarch64" && arch != "arm64" {
		return "", fmt.Errorf("unsupported RTK architecture %s", arch)
	}
	raw, err := u.output("curl", "--fail", "--location", "--max-time", "30", "--proto", "=https", "https://api.github.com/repos/rtk-ai/rtk/releases/latest")
	if err != nil {
		return "", err
	}
	var release struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if err = json.Unmarshal([]byte(raw), &release); err != nil {
		return "", err
	}
	if release.Tag != "" {
		if current, err := u.output("rtk", "--version"); err == nil {
			for _, field := range strings.Fields(current) {
				if field == strings.TrimPrefix(release.Tag, "v") {
					return current, nil
				}
			}
		}
	}
	for _, asset := range release.Assets {
		if asset.Name != name {
			continue
		}
		if !strings.HasPrefix(asset.URL, "https://github.com/rtk-ai/rtk/releases/download/") || !strings.HasPrefix(asset.Digest, "sha256:") || len(asset.Digest) != 71 {
			return "", errors.New("RTK release lacks trusted URL or SHA256 digest")
		}
		dir, err := u.stage()
		if err != nil {
			return "", err
		}
		ok := false
		defer func() {
			if !ok {
				_ = u.command("rm", "-rf", "--", dir)
			}
		}()
		archive := dir + "/rtk.tar.gz"
		if err = u.command("curl", "--fail", "--location", "--max-time", "60", "--proto", "=https", "-o", archive, asset.URL); err != nil {
			return "", err
		}
		digest, err := u.output("sha256sum", archive)
		if err != nil {
			return "", err
		}
		if !strings.HasPrefix(digest, strings.TrimPrefix(asset.Digest, "sha256:")+" ") {
			return "", errors.New("RTK checksum mismatch")
		}
		if err = u.command("tar", "-xzf", archive, "-C", dir); err != nil {
			return "", err
		}
		binary, err := u.output("find", dir, "-type", "f", "-name", "rtk", "-print", "-quit")
		if err != nil {
			return "", err
		}
		if !strings.HasPrefix(binary, dir+"/") || strings.ContainsAny(binary, "\r\n") {
			return "", errors.New("missing RTK binary")
		}
		v, err := u.activate(binary, "rtk")
		ok = err == nil
		return v, err
	}
	return "", errors.New("RTK release has no compatible asset")
}

// methodology updates one methodology for the given agents of a runtime.
func (u updater) methodology(agents []string, toolID string, lock config.Lock) func() (string, error) {
	return func() (string, error) {
		var tool struct {
			Package    string `json:"package"`
			Repository string `json:"repository"`
		}
		if err := json.Unmarshal(lock.Tools[toolID], &tool); err != nil {
			return "", err
		}
		switch {
		case tool.Repository != "":
			return u.plugin(agents, toolID)
		case toolID == "openspec":
			return u.npm(tool.Package, "openspec")
		case tool.Package != "":
			version, err := u.output("npm", "view", tool.Package+"@latest", "version")
			if err != nil {
				return "", err
			}
			if version == "" || strings.ContainsAny(version, " \r\n/") {
				return "", errors.New("invalid methodology version")
			}
			for _, agent := range agents {
				args := []string{"npx", "--yes", tool.Package + "@" + version, "--" + agent, "--global"}
				if agent == "antigravity-cli" {
					args = []string{"env", "ANTIGRAVITY_CONFIG_DIR=" + u.home + "/.gemini/antigravity-cli", "npx", "--yes", tool.Package + "@" + version, "--antigravity", "--global"}
				}
				if err := u.command(args...); err != nil {
					return "", err
				}
			}
			return version, nil
		default:
			return "", fmt.Errorf("unsupported methodology %s", toolID)
		}
	}
}

// supported requires every agent of the entry, including each bundle agent.
func supported(entry config.Entry) bool {
	for _, agent := range entry.AgentIDs() {
		if !config.SupportedAdapter(agent) {
			return false
		}
	}
	return true
}
