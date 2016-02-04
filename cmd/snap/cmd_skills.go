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
	"os"
	"text/tabwriter"

	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
)

type cmdSkills struct {
	Type        string `long:"type" description:"constrain listing to skills of this type"`
	Positionals struct {
		Query SnapAndName `positional-arg-name:"<snap>:<skill>" description:"snap or snap:name" skip-help:"true"`
	} `positional-args:"true"`
}

var (
	shortSkillsHelp = i18n.G("Lists skills in the system")
	longSkillsHelp  = i18n.G(`This command skills in the system.

By default all skills, used and offered by all snaps are displayed.

Skills used and offered by a particular snap can be listed with: snap skills <snap name>`)
)

func init() {
	_, err := parser.AddCommand("skills", shortSkillsHelp, longSkillsHelp, &cmdSkills{})
	if err != nil {
		logger.Panicf("unable to add skills command: %v", err)
	}
}

func (x *cmdSkills) Execute(args []string) error {
	skills, err := Client().AllSkills()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 1, ' ', 0)
	fmt.Fprintln(w, i18n.G("Skill\tGranted To"))
	defer w.Flush()
	for _, skill := range skills {
		// TODO: support filtering by snap:skill
		if x.Type != "" && skill.Type != x.Type {
			continue
		}
		switch len(skill.GrantedTo) {
		case 0:
			fmt.Fprintf(w, "%s:%s\t--\n", skill.Snap, skill.Name)
		default:
			fmt.Fprintf(w, "%s:%s\t%s:%s\n",
				skill.Snap, skill.Name, skill.GrantedTo[0].Snap, skill.GrantedTo[0].Name)
			for i := 1; i < len(skill.GrantedTo); i++ {
				fmt.Fprintf(w, "\t%s:%s\n", skill.GrantedTo[i].Snap, skill.GrantedTo[i].Name)
			}
		}
	}
	return nil
}
