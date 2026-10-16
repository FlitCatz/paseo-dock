package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"paseo-dock/internal/operations"
)

// Legacy lifecycle fixtures exercise the retained schema-1 service path explicitly.
// Production CLI creation maps profiles to schema-2 containers.
func runLegacyFixture(root string, args []string, in io.Reader, out, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "create" && args[0] != "build" && args[0] != "load") {
		return Run(root, args, in, out, stderr)
	}
	a, err := NewApp(root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	a.In, a.Out, a.Err = in, out, stderr
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	f.SetOutput(stderr)
	workspace := f.String("workspace", "", "")
	profile := f.String("profile", "", "")
	profiles := f.String("profiles", "", "")
	utilities := f.String("utilities", "", "")
	apply := f.Bool("apply", false, "")
	if err = f.Parse(args[1:]); err != nil {
		return 1
	}
	setUtilities := false
	f.Visit(func(v *flag.Flag) {
		if v.Name == "utilities" {
			setUtilities = true
		}
	})
	selection, err := a.resolveSelection(*workspace, *profile, *profiles, *utilities, "", "", "", setUtilities)
	if err == nil {
		_, err = a.Service.Execute(context.Background(), operations.Request{Kind: operations.Kind(args[0]), Target: operations.Target{Workspace: *workspace}, Selection: selection, Namespace: "paseo-dock", Memory: "2g", Apply: *apply, PaseoHome: "~/.paseo"}, nil)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
