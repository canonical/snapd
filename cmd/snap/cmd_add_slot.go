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

type cmdAddSlot struct {
	Positionals struct {
		Snap string `positional-arg-name:"snap" description:"name of the snap containing the slot"`
		Slot string `positional-arg-name:"slot" description:"name of the slot within the snap"`
		Type string `positional-arg-name:"type" description:"name of the skill type"`
	} `positional-args:"true" required:"true"`
	Attrs []AttributePair `short:"a" description:"key=value attributes"`
	Apps  []string        `long:"app" description:"list of apps using this slot"`
	Label string          `long:"label" description:"human-friendly label"`
}

var (
	shortAddSlotHelp = i18n.G("Add a skill slot to the system")
	longAddSlotHelp  = i18n.G("This command adds a skill slot to the system")
)

func init() {
	var err error
	if develCommand == nil {
		err = fmt.Errorf("devel command not found")
	} else {
		_, err = develCommand.AddCommand("add-slot", shortAddSlotHelp, longAddSlotHelp, &cmdAddSlot{})
	}
	if err != nil {
		logger.Panicf("unable to add add-slot command: %v", err)
	}
}

func (x *cmdAddSlot) Execute(args []string) error {
	attrs := make(map[string]interface{})
	for k, v := range AttributePairSliceToMap(x.Attrs) {
		attrs[k] = v
	}
	return client.New().AddSlot(&client.Slot{
		Snap:  x.Positionals.Snap,
		Name:  x.Positionals.Slot,
		Type:  x.Positionals.Type,
		Attrs: attrs,
		Apps:  x.Apps,
		Label: x.Label,
	})
}
