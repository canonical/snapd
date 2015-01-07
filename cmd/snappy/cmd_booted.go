package main

import (
	"launchpad.net/snappy-ubuntu/snappy-golang/snappy"
)

type CmdBooted struct {
}

var cmdBooted CmdBooted

func init() {
	Parser.AddCommand("booted",
		"Flag that rootfs booted successfully",
		"Not necessary to run this command manually",
		&cmdBooted)
}

func (x *CmdBooted) Execute(args []string) (err error) {
	return snappy.CmdBooted(args)
}
