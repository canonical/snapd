package main

import (
	"fmt"
	"os"

	"launchpad.net/snappy/priv"
	"launchpad.net/snappy/snappy"
)

type cmdInstall struct {
	AllowUnauthenticated bool `long:"allow-unauthenticated" description:"Install snaps even if the signature can not be verified."`
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

	var flags snappy.InstallFlags
	if x.AllowUnauthenticated {
		flags |= snappy.AllowUnauthenticated
	}

	for _, part := range args {
		fmt.Printf("Installing %s\n", part)
		err = snappy.Install(part, flags)
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
