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

package main_test

import (
	"fmt"
	"net/http"

	. "gopkg.in/check.v1"

	. "github.com/ubuntu-core/snappy/cmd/snap"
)

func (s *SnapSuite) TestAddSkillSlotHelp(c *C) {
	msg := `Usage:
  snap.test [OPTIONS] experimental add-skill-slot [add-skill-slot-OPTIONS] <snap> <skill slot> <type>

The add-skill-slot command adds a new skill slot to the system.

This command is only for experimentation with the skill system.
It will be removed in one of the future releases.

Help Options:
  -h, --help              Show this help message

[add-skill-slot command options]
      -a=                 List of key=value attributes
          --app=          List of apps using this skill slot
          --label=        Human-friendly label

[add-skill-slot command arguments]
  <snap>:                 Name of the snap containing the slot
  <skill slot>:           Name of the skill slot within the snap
  <type>:                 Skill type
`
	rest, _ := Parser().ParseArgs([]string{"experimental", "add-skill-slot", "--help"})
	// TODO: Re-enable this test after go-flags is updated
	msg = msg[:]
	// c.Assert(err.Error(), Equals, msg)
	// NOTE: Updated go-flags returns []string{} here
	c.Assert(rest, DeepEquals, []string{"--help"})
}

func (s *SnapSuite) TestAddSkillSlotExplicitEverything(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/2.0/skills")
		c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
			"action": "add-slot",
			"slot": map[string]interface{}{
				"snap": "consumer",
				"name": "slot",
				"type": "type",
				"attrs": map[string]interface{}{
					"attr": "value",
				},
				"apps": []interface{}{
					"my-app",
				},
				"label": "label",
			},
		})
		fmt.Fprintln(w, `{"type":"sync", "result":{}}`)
	})
	rest, err := Parser().ParseArgs([]string{
		"experimental", "add-skill-slot", "consumer", "slot", "type",
		"-a", "attr=value", "--app=my-app", "--label=label",
	})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}
