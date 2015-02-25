package main

import (
	"fmt"

	"launchpad.net/snappy/snappy"
)

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
	snapPackage, err := snappy.Build(args[0])
	if err != nil {
		return err
	}

	fmt.Printf("Generated '%s' snap\n", snapPackage)
	return nil
}
