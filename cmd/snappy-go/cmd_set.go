package main

import "launchpad.net/snappy/snappy"

type CmdSet struct {
}

var cmdSet CmdSet

const setHelp = `Set properties of system or package

Supported properties are:
  active=VERSION

Example:
  set hello-world active=1.0
`

func init() {
	_, _ = Parser.AddCommand("set",
		"Set properties of system or package",
		setHelp,
		&cmdSet)
}

func (x *CmdSet) Execute(args []string) (err error) {
	return set(args)
}

func set(args []string) (err error) {
	return snappy.SetProperty(args)
}
