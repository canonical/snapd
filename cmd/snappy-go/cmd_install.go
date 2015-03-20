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
		return err
	}
	defer privMutex.Unlock()

	for _, part := range args {
		fmt.Printf("Installing %s\n", part)
		err = snappy.Install(part, 0)
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

	return nil
}
