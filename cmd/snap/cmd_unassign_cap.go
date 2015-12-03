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

type unassignCapOptions struct {
	CapName string `positional-arg-name:"cap-name" description:"name of existing capability"`
}

type cmdUnassignCap struct {
	unassignCapOptions `positional-args:"true" required:"true"`
}

var (
	shortUnassignCapHelp = i18n.G("Unassign a capability")
	longUnassignCapHelp  = i18n.G("This command unassigns a capability from a snap")
)

func init() {
	_, err := parser.AddCommand("unassign-cap", shortUnassignCapHelp, longUnassignCapHelp, &cmdUnassignCap{})
	if err != nil {
		logger.Panicf("unable to add unassign-cap command: %v", err)
	}
}

func (x *cmdUnassignCap) Execute(args []string) error {
	return client.New().UnassignCapability(x.CapName)
}
