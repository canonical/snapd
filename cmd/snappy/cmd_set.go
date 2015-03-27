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

	"launchpad.net/snappy/snappy"
)

type cmdSet struct {
}

const setHelp = `Set properties of system or package

Supported properties are:
  active=VERSION

Example:
  set hello-world active=1.0
`

func init() {
	var cmdSetData cmdSet
	_, _ = parser.AddCommand("set",
		"Set properties of system or package",
		setHelp,
		&cmdSetData)
}

func (x *cmdSet) Execute(args []string) (err error) {
	return set(args)
}

func set(args []string) (err error) {
	pkgname, args, err := parseSetPropertyCmdline(args...)
	if err != nil {
		return err
	}

	return snappy.SetProperty(pkgname, args...)
}

func parseSetPropertyCmdline(args ...string) (pkgname string, out []string, err error) {
	if len(args) < 1 {
		return pkgname, args, fmt.Errorf("Need at least one argument for set")
	}

	// check if the first argument is of the form property=value,
	// if so, the spec says we need to put "ubuntu-core" here
	if strings.Contains(args[0], "=") {
		// go version of prepend()
		args = append([]string{"ubuntu-core"}, args...)
	}
	pkgname = args[0]

	return pkgname, args[1:], nil
}
