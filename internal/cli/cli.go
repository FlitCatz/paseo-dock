package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/charmbracelet/x/term"
	"io"
	"os"
	"os/signal"
	"paseo-dock/internal/config"
	"paseo-dock/internal/operations"
	"paseo-dock/internal/tui"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type App struct {
	*operations.Service
	Selection *config.Selection
}

func NewApp(root string) (*App, error) { s, err := operations.New(root); return &App{Service: s}, err }
func (a *App) BakeConfig(namespace, platforms string) map[string]any {
	return a.Service.BakeConfig(a.Selection, namespace, platforms)
}
func (a *App) selectedRuntime(workspace string) (string, error) {
	return a.Service.SelectedRuntime(workspace)
}
func (a *App) installLauncher(base string, workspaces []string) error {
	return a.Service.InstallLauncher(base, workspaces)
}
func (a *App) create(workspace, namespace, memory string) error {
	return a.submit(operations.Request{Kind: operations.KindCreate, Target: operations.Target{Workspace: workspace}, Namespace: namespace, Memory: memory})
}
func (a *App) submit(r operations.Request) error {
	if a.Selection != nil {
		r.Selection = *a.Selection
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := a.Service.Execute(ctx, r, nil)
	if err != nil && r.Kind != operations.KindVersions {
		for _, t := range result.Remaining {
			fmt.Fprintf(a.Err, "paseo-dock: retained target %s / %s; registry: %s\n", t.Profile, t.ResourceIdentity(), filepath.Join(t.Runtime, "registry.json"))
		}
		if result.NeedsVerification {
			fmt.Fprintln(a.Err, "paseo-dock: partial guest state needs verification before retry.")
		}
	}
	if r.Kind == operations.KindVersions {
		encodeErr := json.NewEncoder(a.Out).Encode(result)
		if err == nil {
			err = encodeErr
		}
	}
	return err
}
func ActiveAdapters(i []config.Installed) map[string]string { return operations.ActiveAdapters(i) }
func ProviderConfig(l string, a map[string]string) map[string]any {
	return operations.ProviderConfig(l, a)
}
func MergeConfig(c, g map[string]any)                  { operations.MergeConfig(c, g) }
func MergeManagedConfig(c, g map[string]any, b string) { operations.MergeManagedConfig(c, g, b) }
func Run(root string, args []string, in io.Reader, out, stderr io.Writer) int {
	if len(args) == 0 {
		if terminalStreams(in, out) {
			args = []string{"tui"}
		} else {
			fmt.Fprintln(stderr, "usage: paseo-dock {build|load|create|register|run|unregister|prune|list|shell|stop|remove|update|versions} [options]")
			return 2
		}
	}
	if args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		fmt.Fprintln(out, "usage: paseo-dock {tui|build|load|create|register|run|unregister|prune|list|shell|stop|remove|update|versions} [options]\nRun paseo-dock COMMAND --help for command options.")
		return 0
	}
	if args[0] == "tui" {
		f := flag.NewFlagSet("tui", flag.ContinueOnError)
		f.SetOutput(out)
		workspace := f.String("workspace", "", "initial workspace path")
		if err := f.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			fmt.Fprintln(stderr, "paseo-dock:", err)
			return 1
		}
		if f.NArg() != 0 {
			fmt.Fprintln(stderr, "paseo-dock: unexpected arguments")
			return 1
		}
		if !terminalStreams(in, out) {
			fmt.Fprintln(stderr, "paseo-dock: tui requires terminal input and output")
			return 1
		}
		app, err := NewApp(root)
		if err == nil {
			app.In, app.Out, app.Err = in, out, stderr
			err = tui.RunDashboard(app.Service, app.Catalog, *workspace, in, out)
		}
		if err != nil {
			fmt.Fprintln(stderr, "paseo-dock:", err)
			return 1
		}
		return 0
	}
	app, err := NewApp(root)
	if err != nil {
		fmt.Fprintln(stderr, "paseo-dock:", err)
		return 1
	}
	app.In, app.Out, app.Err = in, out, stderr
	if err = app.execute(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(stderr, "paseo-dock:", err)
		return 1
	}
	return 0
}
func terminalStreams(in io.Reader, out io.Writer) bool {
	input, ok := in.(*os.File)
	if !ok {
		return false
	}
	output, ok := out.(*os.File)
	return ok && term.IsTerminal(input.Fd()) && term.IsTerminal(output.Fd())
}

// updateUsage states the refresh boundary; see README "업데이트 대상과 제외 대상".
const updateUsage = `usage: paseo-dock update [PROFILE] [options]

Refreshes only paseo-dock-managed tools: Codex, Claude, Antigravity CLI,
installed methodologies, and the RTK/Headroom utilities (sandbox: agents only).
It does not touch OS packages, Node, Python/uv, the base image, the
image-pinned omp/gjc, or the ACP adapters.
Full refresh: edit versions.lock.json, rebuild (create --build), remove and
recreate the target, then log in to each agent again. Recreating loses
in-container logins, sessions and guest files.

`

func (a *App) execute(args []string) error {
	command := args[0]
	if command == "run" {
		return a.runPlugin(args[1:])
	}
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(a.Err)
	if command == "update" {
		f.Usage = func() {
			fmt.Fprint(f.Output(), updateUsage)
			f.PrintDefaults()
		}
	}
	var profile, workspace, platforms, profiles, utilities, selectionPath, agents, methods, categoryText, agent, portsText string
	var bundleName, connectionsText string
	f.StringVar(&categoryText, "category", "", "sandbox or container runtime category")
	f.StringVar(&agent, "agent", "", "agent-only creation or existing agent target")
	namespace := ""
	memory := ""
	var createConfigPath string
	var repository config.Repository
	home := "~/.paseo"
	var push, keep, apply, printBake, buildFirst, interactive, skipBuild, all, dryRun, codexUserNS bool
	var yes, removeRuntimes bool
	if command == "build" || command == "load" || command == "create" {
		f.StringVar(&profiles, "profiles", "", "comma-separated agent-methodology pairs")
		f.StringVar(&profile, "profile", "", "select one agent-methodology pair")
		f.StringVar(&utilities, "utilities", "", "comma-separated utilities; empty selects none")
		f.StringVar(&selectionPath, "selection", "", "read selection JSON file")
		f.StringVar(&agents, "agents", "", "comma-separated agents (requires --methodologies)")
		f.StringVar(&methods, "methodologies", "", "comma-separated methodologies (requires --agents)")
		f.StringVar(&workspace, "workspace", "", "workspace path for saved selection")
		f.StringVar(&bundleName, "name", "", "container name; installs every selected pair into one container")
		f.StringVar(&connectionsText, "connections", "", "comma-separated agent=mode Paseo connections (native, acp, none)")
	}
	switch command {
	case "build", "load":
		f.StringVar(&namespace, "namespace", config.DefaultNamespace, "image namespace")
		if command == "build" {
			f.StringVar(&platforms, "platforms", "", "comma-separated platforms")
			f.BoolVar(&push, "push", false, "publish images")
			f.BoolVar(&printBake, "print-bake", false, "print Bake JSON without running Docker")
		} else {
			f.BoolVar(&keep, "keep-archive", false, "retain image archives")
		}
	case "create":
		f.StringVar(&createConfigPath, "config", "", "read creation options and repository from JSON")
		f.BoolVar(&codexUserNS, "codex-userns", false, "opt in new Codex containers to workspace-write isolation; expands kernel syscall access for all guest processes, no added capabilities")
		f.StringVar(&portsText, "ports", "", "comma-separated host:container ports, bound to 127.0.0.1 (container only)")
		f.StringVar(&memory, "memory", memory, "sandbox-only memory (default 2g); invalid for container")
		f.StringVar(&namespace, "namespace", config.DefaultNamespace, "image namespace")
		f.BoolVar(&buildFirst, "build", false, "build and load selected images before creating")
		f.BoolVar(&apply, "apply", false, "register selected providers and reload Paseo after creating")
		f.StringVar(&home, "paseo-home", home, "Paseo configuration home")
		f.BoolVar(&interactive, "interactive", false, "open the selection TUI (requires a terminal)")
		f.BoolVar(&skipBuild, "skip-build", false, "use existing loaded templates in interactive mode")
	case "register":
		f.StringVar(&workspace, "workspace", "", "workspace path")
		f.StringVar(&home, "paseo-home", home, "Paseo configuration home")
		f.BoolVar(&apply, "apply", false, "merge config and reload Paseo")
	case "unregister":
		f.StringVar(&workspace, "workspace", "", "workspace path (required)")
		f.StringVar(&home, "paseo-home", home, "Paseo configuration home")
	case "prune":
		f.StringVar(&home, "paseo-home", home, "Paseo configuration home")
		f.BoolVar(&dryRun, "dry-run", false, "show missing workspaces without removing registries")
		f.BoolVar(&removeRuntimes, "remove-runtimes", false, "also remove stopped runtimes belonging to missing workspaces")
		f.BoolVar(&yes, "yes", false, "confirm irreversible runtime removal without prompting")
	case "remove":
		f.StringVar(&workspace, "workspace", "", "workspace path (required)")
		f.StringVar(&profile, "profile", "", "exact registry profile key (required)")
		f.StringVar(&home, "paseo-home", home, "Paseo configuration home")
		f.BoolVar(&yes, "yes", false, "confirm irreversible runtime removal without prompting")
	case "list", "shell", "stop", "update", "versions":
		f.StringVar(&workspace, "workspace", "", "workspace path")
		if command != "list" {
			f.StringVar(&profile, "profile", "", "update one profile; omitted updates all active profiles")
		}
		if command == "list" {
			f.BoolVar(&all, "all", false, "list every registered workspace")
		}
	default:
		return fmt.Errorf("unknown command %q", command)
	}
	// Accept flags before or after the positional profile.
	rest := args[1:]
	if (command == "shell" || command == "stop" || command == "update" || command == "versions") && len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		profile = rest[0]
		rest = rest[1:]
	}
	if err := f.Parse(rest); err != nil {
		return err
	}
	if command == "shell" || command == "stop" || command == "update" || command == "versions" {
		if profile == "" && f.NArg() == 1 {
			profile = f.Arg(0)
		} else if f.NArg() != 0 {
			return errors.New("unexpected arguments")
		}
	} else if f.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	if command == "create" && createConfigPath != "" {
		c, err := config.LoadCreateConfig(createConfigPath)
		if err != nil {
			return err
		}
		selection, err := a.Catalog.Normalize(c.Selection())
		if err != nil {
			return fmt.Errorf("create config: %w", err)
		}
		c.CreateSelection = config.CreateSelection(selection)
		if err = applyCreateConfig(f, c); err != nil {
			return err
		}
		repository = c.Repository
	}
	category := config.Category(categoryText)
	if category != "" && category != config.CategorySandbox && category != config.CategoryContainer {
		return fmt.Errorf("invalid category %q", category)
	}
	var ports []config.PortMapping
	if portsText != "" {
		for _, pair := range strings.Split(portsText, ",") {
			h, c, ok := strings.Cut(pair, ":")
			hp, he := strconv.Atoi(h)
			cp, ce := strconv.Atoi(c)
			if !ok || he != nil || ce != nil || hp < 1 || hp > 65535 || cp < 1 || cp > 65535 {
				return errors.New("--ports requires host:container ports between 1 and 65535")
			}
			ports = append(ports, config.PortMapping{HostAddress: "127.0.0.1", HostPort: hp, ContainerPort: cp})
		}
	}
	if command == "build" || command == "load" || command == "create" {
		setUtilities := false
		var userNSOverride *bool
		f.Visit(func(flag *flag.Flag) {
			if flag.Name == "utilities" {
				setUtilities = true
			}
			if flag.Name == "codex-userns" {
				userNSOverride = &codexUserNS
			}
		})
		isTerminal := false
		if input, ok := a.In.(*os.File); ok {
			if output, ok := a.Out.(*os.File); ok {
				isTerminal = term.IsTerminal(input.Fd()) && term.IsTerminal(output.Fd())
			}
		}
		openWizard := command == "create" && (interactive || (isTerminal && profile == "" && profiles == "" && selectionPath == "" && agents == "" && methods == "" && !setUtilities && category == "" && agent == ""))
		if openWizard {
			if !isTerminal {
				return errors.New("--interactive requires terminal input and output")
			}
			if workspace == "" {
				var err error
				workspace, err = os.Getwd()
				if err != nil {
					return err
				}
			}
			var initial config.Selection
			if runtime, err := a.selectedRuntime(workspace); err == nil {
				initial, _ = readSavedSelection(runtime)
			}
			if profile != "" || profiles != "" || selectionPath != "" || agents != "" || methods != "" || setUtilities || category != "" || agent != "" || bundleName != "" || connectionsText != "" {
				var err error
				initial, err = a.creationSelection(category, agent, workspace, profile, profiles, utilities, selectionPath, agents, methods, setUtilities, userNSOverride, bundleName, connectionsText)
				if err != nil {
					return err
				}
			} else if userNSOverride != nil {
				if initial.Schema == 0 {
					// A new interactive selection is validated after choices are made.
					initial = config.Selection{Schema: 2, Category: config.CategoryContainer, CodexUserNS: *userNSOverride}
				} else {
					var err error
					initial, err = a.creationSelection("", "", workspace, "", "", "", "", "", "", false, userNSOverride, bundleName, connectionsText)
					if err != nil {
						return err
					}
				}
			}
			if createConfigPath == "" {
				buildFirst = !skipBuild
			} else if skipBuild {
				buildFirst = false
			}
			result, err := tui.RunCreation(a.Catalog, initial, tui.CreationOptions{Workspace: workspace, Memory: memory, Ports: ports, Repository: repository, PaseoHome: home, Apply: apply, BuildFirst: buildFirst, SelectCategory: createConfigPath != ""}, a.In, a.Out)
			if err != nil {
				return err
			}
			a.Selection = &result.Selection
			if _, err = a.Catalog.Normalize(result.Selection); err != nil {
				return err
			}
			apply = result.Apply
			if result.Category == config.CategoryContainer && memory != "" {
				return config.ValidateMemory(result.Category, memory)
			}
			memory = result.Memory
			if result.Category == config.CategoryContainer {
				ports = result.Ports
			}
		} else {
			s, err := a.creationSelection(category, agent, workspace, profile, profiles, utilities, selectionPath, agents, methods, setUtilities, userNSOverride, bundleName, connectionsText)
			if err != nil {
				return err
			}
			a.Selection = &s
		}
	}

	switch command {
	case "build":
		if printBake {
			return json.NewEncoder(a.Out).Encode(a.BakeConfig(namespace, platforms))
		}
		return a.submit(operations.Request{Kind: operations.KindBuild, Namespace: namespace, Platforms: platforms, Push: push})
	case "load":
		return a.submit(operations.Request{Kind: operations.KindLoad, Namespace: namespace, KeepArchive: keep})
	case "create":
		if err := config.ValidateMemory(a.Selection.Category, memory); err != nil {
			return err
		}
		if a.Selection.CodexUserNS {
			fmt.Fprintln(a.Err, "Codex policy: codex-userns-v1 · workspace-write isolation · expanded kernel syscall access for all guest processes in each opted-in Codex container · no added capabilities or privileged mode.")
		}
		return a.submit(operations.Request{Kind: operations.KindCreate, Target: operations.Target{Workspace: workspace}, Repository: repository, Namespace: namespace, Memory: memory, Ports: ports, BuildFirst: buildFirst, Apply: apply, PaseoHome: home})
	case "prune":
		return a.pruneWithConfirmation(home, dryRun, removeRuntimes, yes)
	case "remove":
		if workspace == "" || profile == "" || agent != "" {
			return errors.New("remove requires --workspace and an exact --profile key")
		}
		reg, targets, err := a.registryTargets(workspace, profile, category, true)
		if err != nil {
			return err
		}
		if len(targets) != 1 || targets[0].Profile != profile {
			return errors.New("remove requires an exact registry --profile key")
		}
		target := targets[0]
		fmt.Fprintf(a.Out, "Remove %s / %s\n  category: %s\n  runtime: %s\n  image: %s\n", reg.Workspace, target.Profile, target.Category, target.ResourceIdentity(), target.Entry.Image)
		printRemovalEffects(a.Out)
		if err := a.confirmRemoval(target.Profile, yes); err != nil {
			return err
		}
		return a.submit(operations.Request{Kind: operations.KindRemove, Target: target, PaseoHome: home})
	case "list":
		if all {
			if category != "" {
				return errors.New("--category with --all is not supported; select a workspace")
			}
			return a.Service.ListAll()
		}
		runtime, err := a.selectedRuntime(workspace)
		if err != nil {
			return err
		}
		var reg config.Registry
		if err = config.ReadJSON(filepath.Join(runtime, "registry.json"), &reg); err != nil {
			return err
		}
		if category != "" {
			for key, e := range reg.Profiles {
				if !categoryMatches(category, e) {
					delete(reg.Profiles, key)
				}
			}
			var active []string
			for _, key := range reg.ActiveProfiles() {
				if _, ok := reg.Profiles[key]; ok {
					active = append(active, key)
				}
			}
			reg.Active = active
		}
		enc := json.NewEncoder(a.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(reg)
	case "shell", "stop", "update", "versions", "register", "unregister":
		if agent != "" {
			if profile != "" {
				return errors.New("use only one target or --agent")
			}
			profile = agent
		}
		single := command == "shell" || command == "stop" || command == "versions"
		reg, targets, err := a.registryTargets(workspace, profile, category, single)
		if err != nil {
			return err
		}
		if command == "stop" && profile == "" {
			return errors.New("stop requires an explicit profile or --agent")
		}
		target := operations.Target{Workspace: workspace, Category: category}
		if len(targets) > 0 {
			target = targets[0]
		}
		if command == "shell" || command == "update" || command == "versions" {
			for _, t := range targets {
				if t.Entry.EffectiveCategory() != config.CategoryContainer {
					fmt.Fprintln(a.Err, "paseo-dock:", command, "uses sbx exec and may start a stopped sandbox (including legacy SBX targets).")
					break
				}
			}
		}
		if command == "shell" {
			// The shared operation dispatch preserves agent-only and legacy updates.
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			_, updateErr := a.Service.Execute(ctx, operations.Request{Kind: operations.KindUpdate, Target: target, AutomaticUpdate: true}, nil)
			cancelErr := ctx.Err()
			if cancelErr != nil {
				return cancelErr
			}
			if updateErr != nil {
				fmt.Fprintln(a.Err, "paseo-dock: update failed; opening shell:", updateErr)
			}
			cmd, err := a.Service.ShellCommand(ctx, target)
			if err != nil {
				return err
			}
			cmd.SetStdin(a.In)
			cmd.SetStdout(a.Out)
			cmd.SetStderr(a.Err)
			return cmd.Run()
		}
		if command == "register" || command == "unregister" {
			if profile != "" {
				return errors.New("provider commands select a workspace/category; use --category")
			}
			var scope []operations.Target
			runtime, runtimeErr := a.selectedRuntime(reg.Workspace)
			if runtimeErr != nil {
				return runtimeErr
			}
			for _, key := range reg.ActiveProfiles() {
				entry := reg.Profiles[key]
				scope = append(scope, operations.Target{Workspace: reg.Workspace, Runtime: runtime, Profile: key, Category: entry.EffectiveCategory(), Entry: entry})
			}

			if category != "" && len(targets) == 0 {
				return errors.New("requested category has no active targets")
			}
			targets = scope
			target = operations.Target{Workspace: reg.Workspace, Runtime: runtime, Category: category}
			if command == "register" {
				fmt.Fprintln(a.Err, "Provider merge applies to the full workspace/Paseo provider union.")
			} else if category != "" {
				fmt.Fprintln(a.Err, "Unregister deactivates category", category, "and merges the full Paseo provider union.")
			}
		}
		r := operations.Request{Kind: operations.Kind(command), Target: target, PaseoHome: home, Apply: apply}
		if !single {
			r.Targets = targets
			if r.Targets == nil {
				r.Targets = []operations.Target{}
			}
		}
		return a.submit(r)
	}
	return nil
}
