package main

import (
	"launchpad.net/snappy/snappy"
)

type cmdHWAssign struct {
	Positional struct {
		PackageName string `positional-arg-name:"package name" description:"Assign hardware to a specific installed package"`
		DevicePath  string `positional-arg-name:"device path" description:"The hardware device path (e.g. /dev/ttyUSB0)"`
	} `positional-args:"yes"`
}

const shortHWAssignHelp = `Assign a hardware device to a package`

const longHWAssignHelp = `This command allows access to a specific hardware device (e.g. /dev/ttyUSB0) for an installed package.`

func init() {
	var cmdHWAssignData cmdHWAssign
	_, _ = parser.AddCommand("hw-assign",
		shortHWAssignHelp,
		longHWAssignHelp,
		&cmdHWAssignData)
}

func (x *cmdHWAssign) Execute(args []string) (err error) {
	if !isRoot() {
		return ErrRequiresRoot
	}

	return snappy.AddHWAccess(x.Positional.PackageName, x.Positional.DevicePath)
}
