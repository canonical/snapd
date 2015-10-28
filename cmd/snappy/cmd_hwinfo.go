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
	"strings"

	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/snappy"
)

type cmdHWInfo struct {
	Positional struct {
		PackageName string `positional-arg-name:"package name"`
	} `positional-args:"yes"`
}

var shortHWInfoHelp = i18n.G("List assigned hardware device for a package")

var longHWInfoHelp = i18n.G("This command list what hardware an installed package can access")

func init() {
	arg, err := parser.AddCommand("hw-info",
		shortHWInfoHelp,
		longHWInfoHelp,
		&cmdHWInfo{})
	if err != nil {
		logger.Panicf("Unable to hwinfo: %v", err)
	}
	addOptionDescription(arg, "package name", i18n.G("List assigned hardware for a specific installed package"))
}

func outputHWAccessForPkgname(pkgname string, writePaths []string) {
	if len(writePaths) == 0 {
		// TRANSLATORS: the %s is a pkgname
		fmt.Printf(i18n.G("'%s:' is not allowed to access additional hardware\n"), pkgname)
	} else {
		// TRANSLATORS: the %s is a pkgname, the second a comma separated list of paths
		fmt.Printf(i18n.G("%s: %s\n"), pkgname, strings.Join(writePaths, ", "))
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

func (x *cmdHWInfo) Execute(args []string) error {
	return withMutex(x.doHWInfo)
}

func (x *cmdHWInfo) doHWInfo() error {
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
