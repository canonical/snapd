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

type cmdRemoveSkill struct {
	Positionals struct {
		Snap string `positional-arg-name:"snap" description:"name of the snap containing the skill"`
		Name string `positional-arg-name:"name" description:"name of the skill slot within the snap"`
	} `positional-args:"true" required:"true"`
}

var (
	shortRemoveSkillHelp = i18n.G("Remove a skill from the system")
	longRemoveSkillHelp  = i18n.G(`This command removes a skill from the system.

This command is only for experimentation with the skill system.
It will be removed in one of the future releases.`)
)

func init() {
	var err error
	if experimentalCommand == nil {
		err = fmt.Errorf("experimental command not found")
	} else {
		_, err = experimentalCommand.AddCommand("remove-skill", shortRemoveSkillHelp, longRemoveSkillHelp, &cmdRemoveSkill{})
	}
	if err != nil {
		logger.Panicf("unable to add remove-skill command: %v", err)
	}
}

func (x *cmdRemoveSkill) Execute(args []string) error {
	return client.New().RemoveSkill(x.Positionals.Snap, x.Positionals.Name)
}
