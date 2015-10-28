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
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snappy"
)

type cmdSet struct {
	args []string
}

var setHelp = i18n.G(`Set properties of system or package

Supported properties are:
  active=VERSION

Example:
  set hello-world active=1.0
`)

func init() {
	_, err := parser.AddCommand("set",
		i18n.G("Set properties of system or package"),
		setHelp,
		&cmdSet{})
	if err != nil {
		logger.Panicf("Unable to set: %v", err)
	}
}

func (x *cmdSet) Execute(args []string) (err error) {
	x.args = args
	return withMutexAndRetry(x.doSet)
}

func (x *cmdSet) doSet() (err error) {
	pkgname, args, err := parseSetPropertyCmdline(x.args...)
	if err != nil {
		return err
	}

	return snappy.SetProperty(pkgname, progress.MakeProgressBar(), args...)
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
