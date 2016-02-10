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

type cmdAddSkill struct {
	Positionals struct {
		Snap string `positional-arg-name:"<snap>" description:"Name of the snap offering the skill"`
		Name string `positional-arg-name:"<skill>" description:"Skill name within the snap"`
		Type string `positional-arg-name:"<type>" description:"Skill type"`
	} `positional-args:"true" required:"true"`
	Attrs []AttributePair `short:"a" description:"List of key=value attributes"`
	Apps  []string        `long:"app" description:"List of apps providing this skill"`
	Label string          `long:"label" description:"Human-friendly label"`
}

var shortAddSkillHelp = i18n.G("Adds a skill to the system")
var longAddSkillHelp = i18n.G(`
The add-skill command adds a new skill to the system.

This command is only for experimentation with the skill system.
It will be removed in one of the future releases.
`)

func init() {
	addExperimentalCommand("add-skill", shortAddSkillHelp, longAddSkillHelp, func() interface{} {
		return &cmdAddSkill{}
	})
}

func (x *cmdAddSkill) Execute(args []string) error {
	attrs := make(map[string]interface{})
	for k, v := range AttributePairSliceToMap(x.Attrs) {
		attrs[k] = v
	}
	return Client().AddSkill(&client.Skill{
		Snap:  x.Positionals.Snap,
		Name:  x.Positionals.Name,
		Type:  x.Positionals.Type,
		Attrs: attrs,
		Apps:  x.Apps,
		Label: x.Label,
	})
}
