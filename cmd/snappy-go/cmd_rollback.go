package main

import (
	"fmt"
	"sort"

	"launchpad.net/snappy/snappy"
)

type cmdRollback struct {
	Positional struct {
		PackageName string `positional-arg-name:"package name" description:"The package to rolblack "`
		Version     string `positional-arg-name:"version" description:"The version to rolblack to"`
	} `positional-args:"yes"`
}

const rollbackHelp = `Rollback to a previous version of a snap
`

func init() {
	var cmdRollbackData cmdRollback
	_, _ = parser.AddCommand("rollback",
		"Rollback to a previous version of a package",
		rollbackHelp,
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
