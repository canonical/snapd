package main

import (
	"launchpad.net/snappy/snappy"
)

type cmdInternalRunHooks struct {
}

func init() {
	var cmdInternalRunHooks cmdInternalRunHooks
	if _, err := parser.AddCommand("internal-run-hooks", "internal", "internal", &cmdInternalRunHooks); err != nil {
		// panic here as something must be terribly wrong if there is an
		// error here
		panic(err)
	}
}

func (x *cmdInternalRunHooks) Execute(args []string) (err error) {
	return snappy.RunHooks()
}
