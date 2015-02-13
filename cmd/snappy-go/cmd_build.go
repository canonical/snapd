package main

import "launchpad.net/snappy/snappy"

type cmdBuild struct {
}

func init() {
	var cmdBuildData cmdBuild
	cmd, _ := parser.AddCommand("build",
		"Build a package",
		"Creates a snapp package",
		&cmdBuildData)

	cmd.Aliases = append(cmd.Aliases, "bu")
}

func (x *cmdBuild) Execute(args []string) (err error) {
	return snappy.Build(args[0])
}
