package main

import (
	"launchpad.net/snappy/logger"
	"launchpad.net/snappy/priv"
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
	privMutex := priv.New()
	if err := privMutex.TryLock(); err != nil {
		return err
	}
	defer privMutex.Unlock()

	parts, err := snappy.InstalledSnapsByType(snappy.SnapTypeCore)
	if err != nil {
		return logger.LogError(err)
	}

	return logger.LogError(parts[0].(*snappy.SystemImagePart).MarkBootSuccessful())
}
