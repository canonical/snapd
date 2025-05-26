// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

import "github.com/snapcore/snapd/sandbox/gpio"

type cmdUnexportChardev struct {
	Args struct {
		ChipLabels string `positional-arg-name:"<gpio-labels>" description:"comma-separated list of source chip label(s) to match"`
		Lines      string `positional-arg-name:"<lines>" description:"comma-separated list of target gpio line(s)"`
		Gadget     string `positional-arg-name:"<gadget>" description:"gadget snap name"`
		Slot       string `positional-arg-name:"<slot>" description:"gpio-chardev slot name"`
	} `positional-args:"yes" required:"true"`
}

var gpioUnexportGadgetChardevChip = gpio.UnexportGadgetChardevChip

func (c *cmdUnexportChardev) Execute(args []string) error {
	return gpioUnexportGadgetChardevChip(c.Args.Gadget, c.Args.Slot)
}
