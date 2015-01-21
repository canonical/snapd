package main

import (
	"launchpad.net/snappy-ubuntu/snappy-golang/snappy"
)

type CmdInfo struct {
}

var cmdInfo CmdInfo

func init() {
	_, _ = Parser.AddCommand("info",
		"Information about your snappy system",
		"Information about your snappy system",
		&cmdInfo)
}

func (x *CmdInfo) Execute(args []string) (err error) {
	return snappy.CmdInfo()
}
