package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snapdtool"
)

func run() error {
	logger.SimpleSetup(nil)

	prog := os.Args[0]

	logger.Debugf("program: %s", prog)

	target := func() string {
		switch filepath.Base(prog) {
		case "snapd":
			return "/usr/lib/snapd/snapd-fips"
		case "snap":
			return "/usr/lib/snapd/snap-fips"
		default:
			return filepath.Join("/usr/lib/snapd", filepath.Base(prog))
		}
	}()

	logger.Debugf("dispatch target: %v", target)
	return snapdtool.DispatchWithFIPS(target)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
