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
	"github.com/ubuntu-core/snappy/client"
	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
)

type assignCapOptions struct {
	CapName  string `positional-arg-name:"cap-name" description:"name of existing capability"`
	SnapName string `positional-arg-name:"snap-name" description:"name of an installed snap"`
	SlotName string `positional-arg-name:"slot-name" description:"name of the slot within the snap (unused)"`
}

type cmdAssignCap struct {
	assignCapOptions `positional-args:"true" required:"true"`
}

var (
	shortAssignCapHelp = i18n.G("Assign a capability to a slot in a snap")
	longAssignCapHelp  = i18n.G("This command assigns a capability to a slot in a snap")
)

func init() {
	_, err := parser.AddCommand("assign-cap", shortAssignCapHelp, longAssignCapHelp, &cmdAssignCap{})
	if err != nil {
		logger.Panicf("unable to add assign-cap command: %v", err)
	}
}

func (x *cmdAssignCap) Execute(args []string) error {
	return client.New().AssignCapability(x.CapName, &client.Assignment{
		SnapName: x.SnapName,
		SlotName: x.SlotName,
	})
}
