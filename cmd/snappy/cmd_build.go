package main

import (
	"launchpad.net/snappy-ubuntu/snappy-golang/snappy"
)

type CmdBuild struct {
}

var cmdBuild CmdBuild

func init() {
	Parser.AddCommand("build",
	"Build a package",
	"Creates a snapp package",
	&cmdBuild)
}

func (x *CmdBuild) Execute(args []string) (err error) {
	return snappy.CmdBuild(args)
}
