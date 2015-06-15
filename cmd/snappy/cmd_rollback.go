// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package main

import (
	"fmt"
	"os"

	"launchpad.net/snappy/logger"
	"launchpad.net/snappy/progress"
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
	_, err := parser.AddCommand("rollback",
		shortRollbackHelp,
		longRollbackHelp,
		&cmdRollback{})
	if err != nil {
		logger.Panicf("Unable to rollback: %v", err)
	}
}

func (x *cmdRollback) Execute(args []string) (err error) {
	return WithMutex(x.doRollback)
}

func (x *cmdRollback) doRollback() error {
	pkg := x.Positional.PackageName
	version := x.Positional.Version
	if pkg == "" {
		return errNeedPackageName
	}

	nowVersion, err := snappy.Rollback(pkg, version, progress.MakeProgressBar())
	if err != nil {
		return err
	}
	fmt.Printf("Setting %s to version %s\n", pkg, nowVersion)

	m := snappy.NewMetaRepository()
	installed, err := m.Installed()
	if err != nil {
		return err
	}

	parts := snappy.FindSnapsByNameAndVersion(pkg, nowVersion, installed)
	showVerboseList(parts, os.Stdout)

	return nil
}
