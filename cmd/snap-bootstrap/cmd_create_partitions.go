// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"github.com/snapcore/snapd/cmd/snap-bootstrap/bootstrap"
)

var bootstrapRun = bootstrap.Run

func init() {
	const (
		short = "Create missing partitions for the device"
		long  = ""
	)

	if _, err := parser.AddCommand("create-partitions", short, long, &cmdCreatePartitions{}); err != nil {
		panic(err)
	}
}

type cmdCreatePartitions struct {
	Positional struct {
		GadgetRoot string `positional-arg-name:"<gadget-root>"`
		Device     string `positional-arg-name:"<device>"`
	} `positional-args:"yes"`
}

func (c *cmdCreatePartitions) Execute(args []string) error {
	// XXX: add options
	options := &bootstrap.Options{}

	return bootstrapRun(c.Positional.GadgetRoot, c.Positional.Device, options)
}
