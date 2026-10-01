package main

import (
	"flag"
	"fmt"
	"os"
	"paseo-sbx/internal/runtimeinstall"
	"runtime"
)

func main() {
	agent := flag.String("agent", "node", "runtime to install: node or antigravity-cli")
	lock := flag.String("lock", "/opt/paseo-sbx/install/versions.lock.json", "versions lock")
	dest := flag.String("destination", "/opt/paseo-sbx/node", "installation path")
	machine := flag.String("arch", runtime.GOARCH, "target architecture")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "install-runtime: unexpected arguments")
		os.Exit(2)
	}
	var err error
	if *agent == "node" {
		err = runtimeinstall.Install(*lock, *dest, *machine)
	} else {
		err = runtimeinstall.InstallAgent(*lock, *dest, *machine, *agent)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "install-runtime:", err)
		os.Exit(1)
	}
	fmt.Println("Installed checksum-verified", *agent, "for", *machine)
}
