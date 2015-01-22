package main

import (
	"launchpad.net/snappy-ubuntu/snappy-golang/snappy"
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
	return snappy.CmdRemove(args)
}
