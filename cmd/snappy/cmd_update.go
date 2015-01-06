package main

import (
	"launchpad.net/snappy-ubuntu/snappy-golang/snappy"
)

type CmdUpdate struct {
}

var cmdUpdate CmdUpdate

func init() {
	cmd, _ := Parser.AddCommand("update",
	"Update all installed parts",
	"Ensures system is running with latest parts",
	&cmdUpdate)

	cmd.Aliases = append(cmd.Aliases, "up")
}

func (x *CmdUpdate) Execute(args []string) (err error) {
	return snappy.CmdUpdate(args)
}
