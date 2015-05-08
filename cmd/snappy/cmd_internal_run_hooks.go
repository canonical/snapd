package main

import (
	"launchpad.net/snappy/logger"
	"launchpad.net/snappy/snappy"
)

type cmdInternalRunHooks struct {
}

func init() {
	_, err := parser.AddCommand("internal-run-hooks",
		"internal",
		"internal",
		&cmdInternalRunHooks{})
	if err != nil {
		logger.LogAndPanic(err)
	}
}

func (x *cmdInternalRunHooks) Execute(args []string) (err error) {
	return snappy.RunHooks()
}
