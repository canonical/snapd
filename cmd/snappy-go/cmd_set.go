package main

import "launchpad.net/snappy/snappy"

type CmdSet struct {
}

var cmdSet CmdSet

func init() {
	_, _ = Parser.AddCommand("set",
		"Set properties of system or package",
		"Set properties of system or package",
		&cmdSet)
}

func (x *CmdSet) Execute(args []string) (err error) {
	return set(args)
}

func set(args []string) (err error) {
	return snappy.SetProperty(args)
}
