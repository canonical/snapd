package main

import (
	"launchpad.net/snappy-ubuntu/snappy-golang/snappy"
)

type CmdSearch struct {
}

var cmdSearch CmdSearch

func init() {
	cmd, _ := Parser.AddCommand("search",
	"Search for packages to install",
	"Query the store for available packages",
	&cmdSearch)

	cmd.Aliases = append(cmd.Aliases, "se")
}

func (x *CmdSearch) Execute(args []string) (err error) {
	return snappy.CmdSearch(args)
}
