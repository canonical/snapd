package main

import (
	"launchpad.net/snappy-ubuntu/snappy-golang/snappy"
)

type CmdInstall struct {
}

var cmdInstall CmdInstall

func init() {
	cmd, _ := Parser.AddCommand("install",
	"Install a snap package",
	"Install a snap package",
	&cmdInstall)

	cmd.Aliases = append(cmd.Aliases, "in")
}

func (x *CmdInstall) Execute(args []string) (err error) {
	return snappy.CmdInstall(args)
}
