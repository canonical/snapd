package main

import (
	"fmt"
	"os"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snapdtool"
)

func init() {
	err := logger.SimpleSetup()
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to activate logging: %v\n", err)
	}
}

func main() {
	snapdtool.ExecInSnapdOrCoreSnap()
	// This point is only reached if reexec did not happen
	fmt.Fprintln(os.Stderr, "AA Prompt listener not implemented")
}
