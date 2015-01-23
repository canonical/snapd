package main

import (
	"launchpad.net/snappy/snappy"
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
	err = snappy.CmdInstall(args)
	if err != nil {
		return err
	}
	// call show versions afterwards
	return snappy.CmdList([]string{}, false, false)
}
