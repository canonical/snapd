package main

import (
	"errors"
	"os"
	"syscall"

	"github.com/jessevdk/go-flags"
)

func init() {
	if os.Getenv("SNAPPY_DEBUG") != "" {
		// FIXME: need a global logger!
		//setupLogger()
	}
}

// fixed errors the command can return
var requiresRootErr = errors.New("command requires sudo (root)")

// Global snappy command-line options
type Options struct {
	// No global options yet
}

var options Options

var Parser = flags.NewParser(&options, flags.Default)

func main() {
	if _, err := Parser.Parse(); err != nil {
		os.Exit(1)
	}
}

func isRoot() bool {
	return syscall.Getuid() == 0
}
