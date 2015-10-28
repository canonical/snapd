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

	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/snappy"
)

type cmdHWUnassign struct {
	Positional struct {
		PackageName string `positional-arg-name:"package name"`
		DevicePath  string `positional-arg-name:"device path"`
	} `required:"true" positional-args:"yes"`
}

var shortHWUnassignHelp = i18n.G("Unassign a hardware device to a package")

var longHWUnassignHelp = i18n.G("This command removes access of a specific hardware device (e.g. /dev/ttyUSB0) for an installed package.")

func init() {
	arg, err := parser.AddCommand("hw-unassign",
		shortHWUnassignHelp,
		longHWUnassignHelp,
		&cmdHWUnassign{})
	if err != nil {
		logger.Panicf("Unable to hwunassign: %v", err)
	}
	addOptionDescription(arg, "package name", i18n.G("Remove hardware from a specific installed package"))
	addOptionDescription(arg, "device path", i18n.G("The hardware device path (e.g. /dev/ttyUSB0)"))
}

func (x *cmdHWUnassign) Execute(args []string) error {
	return withMutexAndRetry(x.doHWUnassign)
}

func (x *cmdHWUnassign) doHWUnassign() error {
	if err := snappy.RemoveHWAccess(x.Positional.PackageName, x.Positional.DevicePath); err != nil {
		return err
	}

	// TRANSLATORS: the first %s is a pkgname, the second %s is a path
	fmt.Printf(i18n.G("'%s' is no longer allowed to access '%s'\n"), x.Positional.PackageName, x.Positional.DevicePath)
	return nil
}
