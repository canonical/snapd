package main

import "launchpad.net/snappy/snappy"

type cmdSet struct {
}

const setHelp = `Set properties of system or package

Supported properties are:
  active=VERSION

Example:
  set hello-world active=1.0
`

func init() {
	var cmdSetData cmdSet
	_, _ = parser.AddCommand("set",
		"Set properties of system or package",
		setHelp,
		&cmdSetData)
}

func (x *cmdSet) Execute(args []string) (err error) {
	return set(args)
}

func set(args []string) (err error) {
	return snappy.ParseSetPropertyCmdline(args...)
}
