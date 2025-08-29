package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snapdtool"
)

const (
	selfExe = "/proc/self/exe"
)

func run() error {
	logger.SimpleSetup(nil)

	prog := os.Args[0]
	progBase := filepath.Base(prog)

	logger.Debugf("argv[0]: %s", prog)

	target := func() string {
		switch progBase {
		case "snapd":
			return "/usr/lib/snapd/snapd-fips"
		case "snap":
			return "/usr/lib/snapd/snap-fips"
		case "snap-repair":
			return "/usr/lib/snapd/snap-repair-fips"
		case "snap-bootstrap":
			return "/usr/lib/snapd/snap-bootstrap-fips"
		default:
			return filepath.Join("/usr/lib/snapd", progBase)
		}
	}()

	if osutil.IsSymlink(filepath.Join(dirs.SnapBinariesDir, progBase)) {
		// magic symlink execution through a symlink in /snap/<foo>, hand it
		// over to snap command for dispatch
		target = "/usr/lib/snapd/snap-fips"
	}

	logger.Debugf("dispatch target: %v", target)
	return snapdtool.DispatchWithFIPS(target)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
