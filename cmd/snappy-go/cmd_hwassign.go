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

	"launchpad.net/snappy/priv"
	"launchpad.net/snappy/snappy"
)

type cmdHWAssign struct {
	Positional struct {
		PackageName string `positional-arg-name:"package name" description:"Assign hardware to a specific installed package"`
		DevicePath  string `positional-arg-name:"device path" description:"The hardware device path (e.g. /dev/ttyUSB0)"`
	} `required:"true" positional-args:"yes"`
}

const shortHWAssignHelp = `Assign a hardware device to a package`

const longHWAssignHelp = `This command adds access to a specific hardware device (e.g. /dev/ttyUSB0) for an installed package.`

func init() {
	var cmdHWAssignData cmdHWAssign
	_, _ = parser.AddCommand("hw-assign",
		shortHWAssignHelp,
		longHWAssignHelp,
		&cmdHWAssignData)
}

func (x *cmdHWAssign) Execute(args []string) (err error) {
	privMutex := priv.New()
	if err := privMutex.TryLock(); err != nil {
		return err
	}
	defer privMutex.Unlock()

	if err := snappy.AddHWAccess(x.Positional.PackageName, x.Positional.DevicePath); err != nil {
		if err == snappy.ErrHWAccessAlreadyAdded {
			fmt.Printf("'%s' previously allowed access to '%s'. Skipping\n", x.Positional.PackageName, x.Positional.DevicePath)
			return nil
		}

		return err
	}

	fmt.Printf("'%s' is now allowed to access '%s'\n", x.Positional.PackageName, x.Positional.DevicePath)
	return nil
}
