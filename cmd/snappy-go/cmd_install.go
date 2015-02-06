package main

import (
	"os"

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
	if !isRoot() {
		return requiresRootErr
	}

	err = snappy.Install(args)
	if err != nil {
		return err
	}
	// call show versions afterwards
	installed, err := snappy.ListInstalled()
	if err != nil {
		return err
	}

	showInstalledList(installed, false, os.Stdout)

	return nil
}
