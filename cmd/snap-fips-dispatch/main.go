package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snapdtool"
)

const (
	selfExe = "/proc/self/exe"
)

func run() error {
	logger.SimpleSetup(nil)

	prog := os.Args[0]

	exe, err := os.Readlink(selfExe)
	if err != nil {
		return err
	}

	logger.Debugf("argv[0]: %s", prog)
	logger.Debugf("exe: %s", exe)

	target := func() string {
		switch filepath.Base(exe) {
		case "snapd":
			return "/usr/lib/snapd/snapd-fips"
		case "snap":
			return "/usr/lib/snapd/snap-fips"
		case "snap-repair":
			return "/usr/lib/snapd/snap-repair-fips"
		case "snap-bootstrap":
			return "/usr/lib/snapd/snap-bootstrap-fips"
		default:
			return filepath.Join("/usr/lib/snapd", filepath.Base(exe))
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
