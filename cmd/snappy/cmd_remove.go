package main

import (
	"fmt"

	"launchpad.net/snappy/snappy"
)

type CmdRemove struct {
}

var cmdRemove CmdRemove

func init() {
	_, _ = Parser.AddCommand("remove",
		"Remove a snapp part",
		"Remove a snapp part",
		&cmdRemove)
}

func (x *CmdRemove) Execute(args []string) (err error) {
	if !isRoot() {
		return requiresRootErr
	}

	for _, part := range args {
		fmt.Printf("Removing %s\n", part)

		if err := snappy.Remove(part); err != nil {
			return err
		}
	}

	return nil
}
