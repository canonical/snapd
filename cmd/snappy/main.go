package main

import (
	"os"

	"github.com/jessevdk/go-flags"
)

func init() {
	if os.Getenv("SNAPPY_DEBUG") != "" {
		// FIXME: need a global logger!
		//setupLogger()
	}
}

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
