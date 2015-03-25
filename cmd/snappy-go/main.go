package main

import (
	"errors"
	"os"

	"launchpad.net/snappy/priv"
	"launchpad.net/snappy/snappy"

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
	if os.Getenv("SNAPPY_DEBUG") != "" {
		// FIXME: need a global logger!
		//setupLogger()
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
