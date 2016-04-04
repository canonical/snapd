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

	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snappy"
)

type cmdRollback struct {
	Positional struct {
		PackageName string `positional-arg-name:"package name"`
		Version     string `positional-arg-name:"version"`
	} `positional-args:"yes"`
}

var shortRollbackHelp = i18n.G("Rollback to a previous version of a package")

var longRollbackHelp = i18n.G("Allows rollback of a snap to a previous installed version. Without any arguments, the previous installed version is selected. It is also possible to specify the version to rollback to as a additional argument.\n")

func init() {
	arg, err := parser.AddCommand("rollback",
		shortRollbackHelp,
		longRollbackHelp,
		&cmdRollback{})
	if err != nil {
		logger.Panicf("Unable to rollback: %v", err)
	}
	addOptionDescription(arg, "package name", i18n.G("The package to rollback "))
	addOptionDescription(arg, "version", i18n.G("The version to rollback to"))
}

func (x *cmdRollback) Execute(args []string) (err error) {
	return withMutexAndRetry(x.doRollback)
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
	// TRANSLATORS: the first %s is a pkgname, the second %s is the new version
	fmt.Printf(i18n.G("Setting %s to version %s\n"), pkg, nowVersion)

	installed, err := (&snappy.Overlord{}).Installed()
	if err != nil {
		return err
	}

	snaps := []*snappy.Snap{}
	for _, installed := range installed {
		if pkg == installed.Name() && nowVersion == installed.Version() {
			snaps = append(snaps, installed)
		}
	}
	showVerboseList(snaps, os.Stdout)

	return nil
}
