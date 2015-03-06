package main

import (
	"launchpad.net/snappy/snappy"
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
	var priv *Privileged

	if priv, err = NewPrivileged(); err != nil {
		return err
	}
	defer func() { err = priv.Stop() }()

	parts, err := snappy.InstalledSnapsByType(snappy.SnapTypeCore)
	if err != nil {
		return err
	}

	return parts[0].(*snappy.SystemImagePart).MarkBootSuccessful()
}
