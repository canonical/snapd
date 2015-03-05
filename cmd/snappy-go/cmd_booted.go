package main

import (
	"launchpad.net/snappy/snappy"
	"launchpad.net/snappy/helpers"
)

type cmdBooted struct {
}

func init() {
	var cmdBootedData cmdBooted
	parser.AddCommand("booted",
		"Flag that rootfs booted successfully",
		"Not necessary to run this command manually",
		&cmdBootedData)
}

func (x *cmdBooted) Execute(args []string) (err error) {
	var lock *helpers.FileLock
	if lock, err = helpers.StartPrivileged(); err != nil {
		return err
	}

	parts, err := snappy.InstalledSnapsByType(snappy.SnapTypeCore)
	if err != nil {
		return err
	}

	err = parts[0].(*snappy.SystemImagePart).MarkBootSuccessful()
	if err != nil {
		return err
	}

	return helpers.StopPrivileged(lock)
}
