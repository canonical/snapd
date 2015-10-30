// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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
	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snappy"
)

type cmdActivate struct {
	Args struct {
		Snap string `positional-arg-name:"snap"`
	} `positional-args:"yes" required:"very yes"`
	activate bool
}

func init() {
	_, err := parser.AddCommand("activate",
		i18n.G(`Activate a package`),
		i18n.G(`Activate a package that has previously been deactivated. If the package is already activated, do nothing.`),
		&cmdActivate{activate: true})
	if err != nil {
		logger.Panicf("Unable to activate: %v", err)
	}

	_, err = parser.AddCommand("deactivate",
		i18n.G(`Deactivate a package`),
		i18n.G(`Deactivate a package. If the package is already deactivated, do nothing.`),
		&cmdActivate{activate: false})
	if err != nil {
		logger.Panicf("Unable to deactivate: %v", err)
	}
}

func (x *cmdActivate) Execute(args []string) error {
	return withMutexAndRetry(x.doActivate)
}

func (x *cmdActivate) doActivate() error {
	return snappy.SetActive(x.Args.Snap, x.activate, progress.MakeProgressBar())
}
