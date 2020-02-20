// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"github.com/snapcore/snapd/cmd/snap-bootstrap/triggerwatch"
)

func init() {
	const (
		short = "Check if the chooser should be run"
		long  = ""
	)

	if _, err := parser.AddCommand("check-chooser", short, long, &cmdCheckChooser{}); err != nil {
		panic(err)
	}
}

var triggerwatchWaitKey = triggerwatch.WaitTriggerKey

type cmdCheckChooser struct{}

func (c *cmdCheckChooser) Execute(args []string) error {
	// TODO:UC20: check in the gadget if there is a hook or some
	// binary we should run for chooser detection/display. This
	// will require some design work and also thinking if/how such
	// a hook can be confined.

	return triggerwatchWaitKey()
}
