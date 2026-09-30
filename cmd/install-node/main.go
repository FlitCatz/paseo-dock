package main

import (
	"flag"
	"fmt"
	"os"
	"paseo-sbx/internal/nodeinstall"
	"runtime"
)

func main() {
	lock := flag.String("lock", "/opt/paseo-sbx/install/versions.lock.json", "versions lock")
	dest := flag.String("destination", "/opt/paseo-sbx/node", "installation path")
	machine := flag.String("arch", runtime.GOARCH, "target architecture")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "install-node: unexpected arguments")
		os.Exit(2)
	}
	if err := nodeinstall.Install(*lock, *dest, *machine); err != nil {
		fmt.Fprintln(os.Stderr, "install-node:", err)
		os.Exit(1)
	}
	fmt.Println("Installed checksum-verified Node.js for", *machine)
}
