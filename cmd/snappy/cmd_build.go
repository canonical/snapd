package main

import "launchpad.net/snappy/snappy"

type CmdBuild struct {
}

var cmdBuild CmdBuild

func init() {
	cmd, _ := Parser.AddCommand("build",
		"Build a package",
		"Creates a snapp package",
		&cmdBuild)

	cmd.Aliases = append(cmd.Aliases, "bu")
}

func (x *CmdBuild) Execute(args []string) (err error) {
	return snappy.Build(args[0])
}
