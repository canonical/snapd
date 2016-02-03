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
	"github.com/ubuntu-core/snappy/logger"
)

type cmdGrant struct {
	Positionals struct {
		SkillSnap string `positional-arg-name:"skill-snap" description:"name of the snap containing the skill"`
		SkillName string `positional-arg-name:"skill-name" description:"name of the skill"`
		SlotSnap  string `positional-arg-name:"slot-snap" description:"name of the snap containing the skill slot"`
		SlotName  string `positional-arg-name:"slot-name" description:"name of the skill slot"`
	} `positional-args:"true" required:"true"`
}

var (
	shortGrantHelp = i18n.G("Grant a skill to a skill slot")
	longGrantHelp  = i18n.G(`This command grants a skill to a skill slot.

Both the skill and the slot must exist and must be of the same type.
`)
)

func init() {
	_, err := parser.AddCommand("grant", shortGrantHelp, longGrantHelp, &cmdGrant{})
	if err != nil {
		logger.Panicf("unable to add grant command: %v", err)
	}
}

func (x *cmdGrant) Execute(args []string) error {
	return client.New().Grant(x.Positionals.SkillSnap, x.Positionals.SkillName, x.Positionals.SlotSnap, x.Positionals.SlotName)
}
