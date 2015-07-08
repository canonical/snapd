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

	"launchpad.net/snappy/i18n"
	"launchpad.net/snappy/logger"
	"launchpad.net/snappy/snappy"
)

type cmdHWAssign struct {
	Positional struct {
		PackageName string `positional-arg-name:"package name"`
		DevicePath  string `positional-arg-name:"device path"`
	} `required:"true" positional-args:"yes"`
}

var shortHWAssignHelp = i18n.G("Assign a hardware device to a package")

var longHWAssignHelp = i18n.G("This command adds access to a specific hardware device (e.g. /dev/ttyUSB0) for an installed package.")

func init() {
	arg, err := parser.AddCommand("hw-assign",
		shortHWAssignHelp,
		longHWAssignHelp,
		&cmdHWAssign{})
	if err != nil {
		logger.Panicf("Unable to hwassign: %v", err)
	}
	addOptionDescriptionOrPanic(arg, "package name", i18n.G("Assign hardware to a specific installed package"))
	addOptionDescriptionOrPanic(arg, "device path", i18n.G("The hardware device path (e.g. /dev/ttyUSB0)"))
}

func (x *cmdHWAssign) Execute(args []string) error {
	return withMutex(x.doHWAssign)
}

func (x *cmdHWAssign) doHWAssign() error {
	if err := snappy.AddHWAccess(x.Positional.PackageName, x.Positional.DevicePath); err != nil {
		if err == snappy.ErrHWAccessAlreadyAdded {
			// TRANSLATORS: the first %s is a pkgname, the second %s is a path
			fmt.Printf(i18n.G("'%s' previously allowed access to '%s'. Skipping\n"), x.Positional.PackageName, x.Positional.DevicePath)
			return nil
		}

		return err
	}

	// TRANSLATORS: the first %s is a pkgname, the second %s is a path
	fmt.Printf(i18n.G("'%s' is now allowed to access '%s'\n"), x.Positional.PackageName, x.Positional.DevicePath)
	return nil
}
