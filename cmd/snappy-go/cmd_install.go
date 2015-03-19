package main

import (
	"fmt"
	"os"

	"launchpad.net/snappy/priv"
	"launchpad.net/snappy/snappy"
)

type cmdInstall struct {
}

func init() {
	var cmdInstallData cmdInstall
	_, _ = parser.AddCommand("install",
		"Install a snap package",
		"Install a snap package",
		&cmdInstallData)
}

func (x *cmdInstall) Execute(args []string) (err error) {
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

	for _, part := range args {
		err = snappy.Install(part)
		if err == snappy.ErrPackageNotFound {
			return fmt.Errorf("No package '%s' for %s", part, ubuntuCoreChannel())
		}
		if err != nil {
			return err
		}
	}

	// call show versions afterwards
	installed, err := snappy.ListInstalled()
	if err != nil {
		return err
	}

	showInstalledList(installed, os.Stdout)

	return err
}
