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
	"paseo-sbx/internal/config"
	"paseo-sbx/internal/operations"
	"paseo-sbx/internal/sandboxupdate"
	"paseo-sbx/internal/tui"
	"path/filepath"
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
	_, err := a.Service.Execute(ctx, r, nil)
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
			fmt.Fprintln(stderr, "usage: paseo-sbx {build|load|create|register|run|unregister|prune|list|shell|stop|update} [options]")
			return 2
		}
	}
	if args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		fmt.Fprintln(out, "usage: paseo-sbx {tui|build|load|create|register|run|unregister|prune|list|shell|stop|update} [options]\nRun paseo-sbx COMMAND --help for command options.")
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
			fmt.Fprintln(stderr, "paseo-sbx:", err)
			return 1
		}
		if f.NArg() != 0 {
			fmt.Fprintln(stderr, "paseo-sbx: unexpected arguments")
			return 1
		}
		if !terminalStreams(in, out) {
			fmt.Fprintln(stderr, "paseo-sbx: tui requires terminal input and output")
			return 1
		}
		app, err := NewApp(root)
		if err == nil {
			app.In, app.Out, app.Err = in, out, stderr
			err = tui.RunDashboard(app.Service, app.Catalog, *workspace, in, out)
		}
		if err != nil {
			fmt.Fprintln(stderr, "paseo-sbx:", err)
			return 1
		}
		return 0
	}
	app, err := NewApp(root)
	if err != nil {
		fmt.Fprintln(stderr, "paseo-sbx:", err)
		return 1
	}
	app.In, app.Out, app.Err = in, out, stderr
	if err = app.execute(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(stderr, "paseo-sbx:", err)
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
func (a *App) execute(args []string) error {
	command := args[0]
	if command == "run" {
		return a.runPlugin(args[1:])
	}
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(a.Err)
	var profile, workspace, platforms, profiles, utilities, selectionPath, agents, methods string
	namespace := "paseo-sbx"
	memory := "2g"
	home := "~/.paseo"
	var push, keep, apply, printBake, buildFirst, interactive, skipBuild, all, dryRun bool
	if command == "build" || command == "load" || command == "create" {
		f.StringVar(&profiles, "profiles", "", "comma-separated agent-methodology pairs")
		f.StringVar(&profile, "profile", "", "select one agent-methodology pair")
		f.StringVar(&utilities, "utilities", "", "comma-separated utilities; empty selects none")
		f.StringVar(&selectionPath, "selection", "", "read selection JSON file")
		f.StringVar(&agents, "agents", "", "comma-separated agents (requires --methodologies)")
		f.StringVar(&methods, "methodologies", "", "comma-separated methodologies (requires --agents)")
		f.StringVar(&workspace, "workspace", "", "workspace path for saved selection")
	}
	switch command {
	case "build", "load":
		f.StringVar(&namespace, "namespace", namespace, "image namespace")
		if command == "build" {
			f.StringVar(&platforms, "platforms", "", "comma-separated platforms")
			f.BoolVar(&push, "push", false, "publish images")
			f.BoolVar(&printBake, "print-bake", false, "print Bake JSON without running Docker")
		} else {
			f.BoolVar(&keep, "keep-archive", false, "retain image archives")
		}
	case "create":
		f.StringVar(&memory, "memory", memory, "memory per sandbox")
		f.StringVar(&namespace, "namespace", namespace, "image namespace")
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
	case "list", "shell", "stop", "update":
		f.StringVar(&workspace, "workspace", "", "workspace path")
		if command == "update" {
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
	if (command == "shell" || command == "stop" || command == "update") && len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		profile = rest[0]
		rest = rest[1:]
	}
	if err := f.Parse(rest); err != nil {
		return err
	}
	if command == "shell" || command == "stop" || command == "update" {
		if profile == "" && f.NArg() == 1 {
			profile = f.Arg(0)
		} else if f.NArg() != 0 {
			return errors.New("unexpected arguments")
		}
	} else if f.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	if _, ok := a.Catalog.Pair(profile); profile != "" && !ok && command != "shell" && command != "stop" {
		return fmt.Errorf("invalid profile %q", profile)
	}
	if command == "stop" && profile == "" {
		return errors.New("stop requires an explicit profile")
	}
	if command == "build" || command == "load" || command == "create" {
		setUtilities := false
		f.Visit(func(flag *flag.Flag) {
			if flag.Name == "utilities" {
				setUtilities = true
			}
		})
		isTerminal := false
		if input, ok := a.In.(*os.File); ok {
			if output, ok := a.Out.(*os.File); ok {
				isTerminal = term.IsTerminal(input.Fd()) && term.IsTerminal(output.Fd())
			}
		}
		openWizard := command == "create" && (interactive || (isTerminal && profile == "" && profiles == "" && selectionPath == "" && agents == "" && methods == "" && !setUtilities))
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
			if profile != "" || profiles != "" || selectionPath != "" || agents != "" || methods != "" || setUtilities {
				var err error
				initial, err = a.resolveSelection(workspace, profile, profiles, utilities, selectionPath, agents, methods, setUtilities)
				if err != nil {
					return err
				}
			}
			result, err := tui.Run(a.Catalog, initial, workspace, memory, a.In, a.Out, apply)
			if err != nil {
				return err
			}
			a.Selection = &result.Selection
			// Reuse CLI validation for locked versions before any Docker action.
			_, err = a.resolveSelection(workspace, "", strings.Join(result.Selection.Profiles(), ","), strings.Join(result.Selection.Utilities, ","), "", "", "", true)
			if err != nil {
				return err
			}
			apply = apply || result.Apply
			buildFirst = !skipBuild
		} else {
			s, err := a.resolveSelection(workspace, profile, profiles, utilities, selectionPath, agents, methods, setUtilities)
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
		return a.submit(operations.Request{Kind: operations.KindCreate, Target: operations.Target{Workspace: workspace}, Namespace: namespace, Memory: memory, BuildFirst: buildFirst, Apply: apply, PaseoHome: home})
	case "unregister":
		return a.submit(operations.Request{Kind: operations.KindUnregister, Target: operations.Target{Workspace: workspace}, PaseoHome: home})
	case "prune":
		return a.submit(operations.Request{Kind: operations.KindPrune, PaseoHome: home, DryRun: dryRun})
	case "register":
		return a.submit(operations.Request{Kind: operations.KindRegister, Target: operations.Target{Workspace: workspace}, PaseoHome: home, Apply: apply})
	case "stop", "update":
		return a.submit(operations.Request{Kind: operations.Kind(command), Target: operations.Target{Workspace: workspace, Profile: profile}})
	case "list", "shell":
		if command == "list" && all {
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
		if command == "list" {
			enc := json.NewEncoder(a.Out)
			enc.SetIndent("", "  ")
			return enc.Encode(reg)
		}
		entry, ok := reg.Profiles[profile]
		if !ok {
			return errors.New("profile missing from registry")
		}
		// Catch signals only during the background update. The interactive shell
		// retains the terminal's signal/process-group ownership afterward.
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		err = sandboxupdate.UpdateContext(ctx, runtime, entry, false, a.Err, nil)
		cancelErr := ctx.Err()
		stop()
		if cancelErr != nil {
			return cancelErr
		}
		if err != nil {
			fmt.Fprintln(a.Err, "paseo-sbx: update failed; opening shell:", err)
		}
		cmd, err := a.Service.ShellCommand(context.Background(), operations.Target{Workspace: reg.Workspace, Runtime: runtime, Profile: profile, Entry: entry})
		if err != nil {
			return err
		}
		cmd.Stdin = a.In
		cmd.Stdout = a.Out
		cmd.Stderr = a.Err
		return cmd.Run()
	}
	return nil
}
