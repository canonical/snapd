package main

import (
	"errors"
	"fmt"
	"os"

	"launchpad.net/snappy/priv"
	"launchpad.net/snappy/snappy"

	"launchpad.net/snappy/logger"

	"github.com/jessevdk/go-flags"
)

// fixed errors the command can return
var ErrRequiresRoot = errors.New("command requires sudo (root)")

type options struct {
	// No global options yet
}

var optionsData options

var parser = flags.NewParser(&optionsData, flags.Default)

func init() {
	err := logger.ActivateLogger()
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to activate logging: %s\n", err)
	}
}

func main() {
	if _, err := parser.Parse(); err != nil {
		if err == priv.ErrNeedRoot {
			// make the generic root error more specific for
			// the CLI user.
			err = snappy.ErrNeedRoot
		}
		os.Exit(1)
	}
}
