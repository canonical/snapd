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

type removeCapOptions struct {
	Name string `positional-arg-name:"name" description:"unique capability name"`
}

type cmdRemoveCap struct {
	removeCapOptions `positional-args:"true" required:"true"`
}

var (
	shortRemoveCapHelp = i18n.G("Remove a capability from the system")
	longRemoveCapHelp  = i18n.G("This command removes a capability from the system")
)

func init() {
	_, err := parser.AddCommand("remove-cap", shortRemoveCapHelp, longRemoveCapHelp, &cmdRemoveCap{})
	if err != nil {
		logger.Panicf("unable to add remove-cap command: %v", err)
	}
}

func (x *cmdRemoveCap) Execute(args []string) error {
	return client.New().RemoveCapability(x.Name)
}
