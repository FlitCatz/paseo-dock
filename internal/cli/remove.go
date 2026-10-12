package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/charmbracelet/x/term"
	"paseo-sbx/internal/operations"
)

func printRemovalEffects(out io.Writer) {
	fmt.Fprintln(out, "Removal is irreversible: deletes the runtime, its login state, sessions, guest files and sandbox secrets, registry entries, update/version records and managed providers.")
	fmt.Fprintln(out, "Workspace folders, images/templates and other profiles are preserved. Running targets must be stopped first; unknown state blocks removal.")
}

func (a *App) removalInputAllowed(yes bool) error {
	if yes {
		return nil
	}
	input, ok := a.In.(*os.File)
	if !ok || !term.IsTerminal(input.Fd()) {
		return errors.New("runtime removal requires --yes when stdin is not a TTY")
	}
	return nil
}

func (a *App) confirmRemoval(expected string, yes bool) error {
	if err := a.removalInputAllowed(yes); err != nil {
		return err
	}
	if yes {
		return nil
	}
	fmt.Fprintf(a.Out, "Type %q to confirm removal: ", expected)
	line, err := bufio.NewReader(a.In).ReadString('\n')
	if err != nil {
		return fmt.Errorf("read removal confirmation: %w", err)
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if line != expected {
		return errors.New("removal confirmation did not match; nothing removed")
	}
	return nil
}

func (a *App) pruneWithConfirmation(home string, dryRun, removeRuntimes, yes bool) error {
	r := operations.Request{Kind: operations.KindPrune, PaseoHome: home, DryRun: dryRun, RemoveRuntimes: removeRuntimes}
	if !removeRuntimes || dryRun {
		return a.submit(r)
	}
	if err := a.removalInputAllowed(yes); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	preview := r
	preview.DryRun = true
	result, err := a.Service.Execute(ctx, preview, nil)
	stop()
	if err != nil {
		return err
	}
	if len(result.Missing) == 0 {
		return nil
	}
	if len(result.Remaining) > 0 {
		return errors.New("runtime pruning blocked: stop first for running targets; state unknown targets cannot be removed")
	}
	printRemovalEffects(a.Out)
	if err := a.confirmRemoval("prune", yes); err != nil {
		return err
	}
	r.PruneTargets = result.Missing
	return a.submit(r)
}
