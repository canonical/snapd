package main

import (
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
		if err == priv.ErrNeedRoot {
			err = snappy.ErrNeedRoot
		}
		return err
	}
	defer func() {
		err = privMutex.Unlock()
		if err == priv.ErrNeedRoot {
			err = snappy.ErrNeedRoot
		}
	}()

	parts, err := snappy.InstalledSnapsByType(snappy.SnapTypeCore)
	if err != nil {
		return err
	}

	return parts[0].(*snappy.SystemImagePart).MarkBootSuccessful()
}
