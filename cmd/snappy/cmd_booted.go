package main

import "launchpad.net/snappy/snappy"

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
	parts, err := snappy.InstalledSnapsByType(snappy.SnapTypeCore)
	if err != nil {
		return err
	}

	return parts[0].(*snappy.SystemImagePart).MarkBootSuccessful()
}
