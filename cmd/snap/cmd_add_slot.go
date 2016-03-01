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
	"github.com/ubuntu-core/snappy/client"
	"github.com/ubuntu-core/snappy/i18n"
)

type cmdAddSlot struct {
	Positionals struct {
		Snap      string `positional-arg-name:"<snap>" description:"Name of the snap containing the slot"`
		Slot      string `positional-arg-name:"<slot>" description:"Name of the slot within the snap"`
		Interface string `positional-arg-name:"<interface>" description:"Interface name"`
	} `positional-args:"true" required:"true"`
	Attrs []AttributePair `short:"a" description:"List of key=value attributes"`
	Apps  []string        `long:"app" description:"List of apps using this slot"`
	Label string          `long:"label" description:"Human-friendly label"`
}

var shortAddSlotHelp = i18n.G("Adds a slot to the system")
var longAddSlotHelp = i18n.G(`
The add-slot command adds a new slot to the system.

This command is only for experimentation with interfaces.
It will be removed in one of the future releases.
`)

func init() {
	addExperimentalCommand("add-slot", shortAddSlotHelp, longAddSlotHelp, func() interface{} {
		return &cmdAddSlot{}
	})
}

func (x *cmdAddSlot) Execute(args []string) error {
	attrs := make(map[string]interface{})
	for k, v := range AttributePairSliceToMap(x.Attrs) {
		attrs[k] = v
	}
	return Client().AddSlot(&client.Slot{
		Snap:      x.Positionals.Snap,
		Name:      x.Positionals.Slot,
		Interface: x.Positionals.Interface,
		Attrs:     attrs,
		Apps:      x.Apps,
		Label:     x.Label,
	})
}
