package main

import (
	"fmt"
	"strings"

	"launchpad.net/snappy/priv"
	"launchpad.net/snappy/snappy"
)

type cmdHWInfo struct {
	Positional struct {
		PackageName string `positional-arg-name:"package name" description:"List assigned hardware for a specific installed package"`
	} `positional-args:"yes"`
}

const shortHWInfoHelp = `List assigned hardware device for a package`

const longHWInfoHelp = `This command list what hardware an installed package can access`

func init() {
	var cmdHWInfoData cmdHWInfo
	_, _ = parser.AddCommand("hw-info",
		shortHWInfoHelp,
		longHWInfoHelp,
		&cmdHWInfoData)
}

func outputHWAccessForPkgname(pkgname string, writePaths []string) {
	if len(writePaths) == 0 {
		fmt.Printf("'%s:' is not allowed to access additional hardware\n", pkgname)
	} else {
		fmt.Printf("%s: %s\n", pkgname, strings.Join(writePaths, ", "))
	}
}

func outputHWAccessForAll() error {
	installed, err := snappy.ListInstalled()
	if err != nil {
		return err
	}

	for _, snap := range installed {
		writePaths, err := snappy.ListHWAccess(snap.Name())
		if err == nil && len(writePaths) > 0 {
			outputHWAccessForPkgname(snap.Name(), writePaths)
		}
	}

	return nil
}

func (x *cmdHWInfo) Execute(args []string) (err error) {
	privMutex := priv.New()
	if err := privMutex.TryLock(); err != nil {
		return err
	}
	defer privMutex.Unlock()

	// use specific package
	pkgname := x.Positional.PackageName
	if pkgname != "" {
		writePaths, err := snappy.ListHWAccess(pkgname)
		if err != nil {
			return err
		}
		outputHWAccessForPkgname(pkgname, writePaths)
		return nil
	}

	// no package -> show additional access for all installed snaps
	return outputHWAccessForAll()
}
