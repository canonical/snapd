package main

import (
	"fmt"

	"launchpad.net/snappy/snappy"
)

type cmdHWUnassign struct {
	Positional struct {
		PackageName string `positional-arg-name:"package name" description:"Remove hardware from a specific installed package"`
		DevicePath  string `positional-arg-name:"device path" description:"The hardware device path (e.g. /dev/ttyUSB0)"`
	} `positional-args:"yes"`
}

const shortHWUnassignHelp = `Unassign a hardware device to a package`

const longHWUnassignHelp = `This command removes access of a specific hardware device (e.g. /dev/ttyUSB0) for an installed package.`

func init() {
	var cmdHWUnassignData cmdHWUnassign
	_, _ = parser.AddCommand("hw-unassign",
		shortHWUnassignHelp,
		longHWUnassignHelp,
		&cmdHWUnassignData)
}

func (x *cmdHWUnassign) Execute(args []string) (err error) {
	if !isRoot() {
		return ErrRequiresRoot
	}

	if err := snappy.RemoveHWAccess(x.Positional.PackageName, x.Positional.DevicePath); err != nil {
		return err
	}

	fmt.Printf("'%s' is no longer allowed to access '%s'\n", x.Positional.PackageName, x.Positional.DevicePath)
	return nil
}
