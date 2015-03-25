package main

import (
	"fmt"
	"sort"

	"launchpad.net/snappy/snappy"
)

type cmdRollback struct {
	Positional struct {
		PackageName string `positional-arg-name:"package name" description:"The package to rollback "`
		Version     string `positional-arg-name:"version" description:"The version to rollback to"`
	} `positional-args:"yes"`
}

const shortRollbackHelp = "Rollback to a previous version of a package"

const longRollbackHelp = `Allows rollback of a snap to a previous installed version. Without any arguments, the previous installed version is selected. It is also possible to specify the version to rollback to as a additional argument.
`

func init() {
	var cmdRollbackData cmdRollback
	_, _ = parser.AddCommand("rollback",
		shortRollbackHelp,
		longRollbackHelp,
		&cmdRollbackData)
}

func (x *cmdRollback) Execute(args []string) (err error) {
	if !isRoot() {
		return ErrRequiresRoot
	}

	pkg := x.Positional.PackageName
	ver := x.Positional.Version
	if pkg == "" {
		return errNeedPackageName
	}
	// no version specified, find the previous one
	if ver == "" {
		m := snappy.NewMetaRepository()
		installed, err := m.Installed()
		if err != nil {
			return err
		}
		snaps := snappy.FindSnapsByName(pkg, installed)
		if len(snaps) < 2 {
			return fmt.Errorf("no version to rollback to")
		}
		sort.Sort(snappy.BySnapVersion(snaps))
		// -1 is the most recent, -2 the previous one
		ver = snaps[len(snaps)-2].Version()
	}

	return snappy.MakeSnapActiveByNameAndVersion(pkg, ver)
}
