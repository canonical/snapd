// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

	"github.com/ubuntu-core/snappy/client"
	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
)

type cmdRemoveSlot struct {
	Positionals struct {
		Snap string `positional-arg-name:"snap" description:"name of the snap containing the skill slot"`
		Slot string `positional-arg-name:"slot" description:"name of the skill slot within the snap"`
	} `positional-args:"true" required:"true"`
}

var (
	shortRemoveSlotHelp = i18n.G("Remove a skill slot from the system")
	longRemoveSlotHelp  = i18n.G("This command removes a skill slot from the system")
)

func init() {
	var err error
	if develCommand == nil {
		err = fmt.Errorf("devel command not found")
	} else {
		_, err = develCommand.AddCommand("remove-slot", shortRemoveSlotHelp, longRemoveSlotHelp, &cmdRemoveSlot{})
	}
	if err != nil {
		logger.Panicf("unable to add remove-slot command: %v", err)
	}
}

func (x *cmdRemoveSlot) Execute(args []string) error {
	return client.New().RemoveSlot(x.Positionals.Snap, x.Positionals.Slot)
}
