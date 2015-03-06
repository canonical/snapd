package main

import (
	"errors"
	"log"
	"os"
	"syscall"

	logger "launchpad.net/snappy/logger"

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
	snappyLogger := logger.New()

	// This will affect all subsequent log.* calls (in all modules).
	log.SetOutput(snappyLogger)
}

func main() {
	if _, err := parser.Parse(); err != nil {
		os.Exit(1)
	}
}

func isRoot() bool {
	return syscall.Getuid() == 0
}
