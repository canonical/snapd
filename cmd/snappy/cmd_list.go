package main

import (
	"launchpad.net/snappy-ubuntu/snappy-golang/snappy"
)

type CmdList struct {
	Updates []bool `short:"u" long:"updates" description:"Show available updates"`
}

var cmdList CmdList

func init() {
	Parser.AddCommand("list",
	"List installed parts",
	"Shows all installed parts",
	&cmdList)
}

func (x *CmdList) Execute(args []string) (err error) {
	return snappy.CmdList(args)
}
