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
	"github.com/ubuntu-core/snappy/i18n"
)

type cmdRemoveSkillSlot struct {
	Positionals struct {
		Snap string `positional-arg-name:"snap" description:"name of the snap containing the skill slot"`
		Name string `positional-arg-name:"name" description:"name of the skill slot within the snap"`
	} `positional-args:"true" required:"true"`
}

var shortRemoveSkillSlotHelp = i18n.G("Remove a skill slot from the system")
var longRemoveSkillSlotHelp = i18n.G(`This command removes a skill slot from the system.

This command is only for experimentation with the skill system.
It will be removed in one of the future releases.`)

func init() {
	experimentalCommands = append(experimentalCommands, cmdInfo{
		name:      "remove-skill-slot",
		shortHelp: shortRemoveSkillSlotHelp,
		longHelp:  longRemoveSkillSlotHelp,
		callback:  func() interface{} { return &cmdRemoveSkillSlot{} },
	})
}

func (x *cmdRemoveSkillSlot) Execute(args []string) error {
	return Client().RemoveSlot(x.Positionals.Snap, x.Positionals.Name)
}
