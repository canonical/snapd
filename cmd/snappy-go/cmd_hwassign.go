package main

import (
	"fmt"

	"launchpad.net/snappy/snappy"
)

type cmdHWAssign struct {
	Positional struct {
		PackageName string `positional-arg-name:"package name" description:"Assign hardware to a specific installed package"`
		DevicePath  string `positional-arg-name:"device path" description:"The hardware device path (e.g. /dev/ttyUSB0)"`
	} `required:"true" positional-args:"yes"`
}

const shortHWAssignHelp = `Assign a hardware device to a package`

const longHWAssignHelp = `This command adds access to a specific hardware device (e.g. /dev/ttyUSB0) for an installed package.`

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

	if err := snappy.AddHWAccess(x.Positional.PackageName, x.Positional.DevicePath); err != nil {
		if err == snappy.ErrHWAccessAlreadyAdded {
			fmt.Printf("'%s' previously allowed access to '%s'. Skipping\n", x.Positional.PackageName, x.Positional.DevicePath)
			return nil
		}

		return err
	}

	fmt.Printf("'%s' is now allowed to access '%s'\n", x.Positional.PackageName, x.Positional.DevicePath)
	return nil
}
