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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	versionlock "paseo-dock"
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
	lock       config.Lock
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
	u := updater{ctx: ctx, run: run, lock: versionlock.Load()}
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
	// The guest manifest identifies installed components, never trusted update pins.
	raw, err := u.output("cat", home+"/.local/share/paseo-dock/installed.json")
	var installed struct {
		Lock json.RawMessage `json:"lock"`
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
			step("methodology:"+tool, u.methodology(agentsByTool[tool], tool, u.lock))
		}
	} else if entry.Tool != "native" {
		step("methodology", u.methodology([]string{entry.Agent}, entry.Tool, u.lock))
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

// npm fails closed until the build lock carries an independently pinned package
// checksum. A registry response cannot authorize its own installation payload.
func (u updater) npm(pkg, binary string) (string, error) {
	version := ""
	switch pkg {
	case "@openai/codex":
		version = u.lock.Agents["codex"]
	case "@anthropic-ai/claude-code":
		version = u.lock.Agents["claude"]
	default:
		for _, raw := range u.lock.Tools {
			var tool struct{ Package, Version string }
			if json.Unmarshal(raw, &tool) == nil && tool.Package == pkg {
				version = tool.Version
			}
		}
	}
	if version == "" {
		return "", fmt.Errorf("missing pinned version for %s in versions.lock.json", pkg)
	}
	if current, err := u.output(binary, "--version"); err == nil {
		for _, field := range strings.Fields(current) {
			if field == version {
				return current, nil
			}
		}
	}
	return "", fmt.Errorf("%s@%s lacks a pinned checksum in versions.lock.json; rebuild the image", pkg, version)
}

// plugin fetches a shared plugin checkout once, then reinstalls it for each
// agent; on failure it restores the old revision for every agent touched.
func (u updater) plugin(agents []string, tool string) (string, error) {
	var pin struct{ Revision string }
	if err := json.Unmarshal(u.lock.Tools[tool], &pin); err != nil || len(pin.Revision) != 40 {
		return "", fmt.Errorf("missing pinned revision for %s in versions.lock.json", tool)
	}
	revision := pin.Revision
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
	if err = u.command("git", "-C", dir, "fetch", "--depth", "1", "origin", revision); err != nil {
		return "", err
	}
	next, err := u.output("git", "-C", dir, "rev-parse", "FETCH_HEAD")
	if err != nil {
		return "", err
	}
	if next != revision {
		return "", errors.New("plugin fetched revision differs from versions.lock.json")
	}
	if next == old {
		return old, nil
	}
	if err = u.command("git", "-C", dir, "checkout", "--detach", revision); err != nil {
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
	if arch == "arm64" {
		arch = "aarch64"
	}
	var artifact struct {
		Assets map[string]struct{ URL, SHA512 string }
	}
	if err := json.Unmarshal(u.lock.AgentArtifacts["antigravity-cli"], &artifact); err != nil {
		return "", fmt.Errorf("read pinned Antigravity artifact: %w", err)
	}
	asset := artifact.Assets[arch]
	manifest := struct{ Version, URL, SHA512 string }{u.lock.Agents["antigravity-cli"], asset.URL, asset.SHA512}
	if manifest.Version == "" || !strings.HasPrefix(manifest.URL, "https://storage.googleapis.com/antigravity-public/antigravity-cli/") || len(manifest.SHA512) != 128 {
		return "", errors.New("missing pinned Antigravity version, URL or checksum in versions.lock.json")
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

// The lock has no complete wheel/dependency hashes. Do not resolve an unpinned
// environment at runtime, even when the top-level package version is pinned.
func (u updater) headroom(pkg string) (string, error) {
	return "", errors.New("Headroom lacks pinned package and dependency checksums in versions.lock.json; rebuild the image")
}

func (u updater) rtk() (string, error) {
	arch, err := u.output("uname", "-m")
	if err != nil {
		return "", err
	}
	if arch == "arm64" {
		arch = "aarch64"
	}
	var pin struct {
		Version string
		Assets  map[string]struct{ URL, SHA256 string }
	}
	if err := json.Unmarshal(u.lock.Utilities["rtk"], &pin); err != nil {
		return "", fmt.Errorf("read pinned RTK artifact: %w", err)
	}
	asset := pin.Assets[arch]
	if pin.Version == "" || !strings.HasPrefix(asset.URL, "https://github.com/rtk-ai/rtk/releases/download/v"+pin.Version+"/") || len(asset.SHA256) != 64 {
		return "", errors.New("missing pinned RTK version, URL or SHA256 checksum in versions.lock.json")
	}
	if current, err := u.output("rtk", "--version"); err == nil {
		for _, field := range strings.Fields(current) {
			if field == pin.Version {
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
	archive := dir + "/rtk.tar.gz"
	if err = u.command("curl", "--fail", "--location", "--max-time", "60", "--proto", "=https", "-o", archive, asset.URL); err != nil {
		return "", err
	}
	digest, err := u.output("sha256sum", archive)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(digest, asset.SHA256+" ") {
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
			return "", fmt.Errorf("%s lacks a pinned package checksum in versions.lock.json; rebuild the image", tool.Package)
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
