package main

import (
	"launchpad.net/snappy-ubuntu/snappy-golang/snappy"
)

type CmdList struct {
	Updates bool `short:"u" long:"updates" description:"Show available updates"`
	ShowAll bool `short:"a" long:"all" description:"Show all parts"`
}

var cmdList CmdList

func init() {
	cmd, _ := Parser.AddCommand("list",
		"List installed parts",
		"Shows all installed parts",
		&cmdList)

	cmd.Aliases = append(cmd.Aliases, "li")
}

func (x *CmdList) Execute(args []string) (err error) {
	return snappy.CmdList(args, x.ShowAll)
}
