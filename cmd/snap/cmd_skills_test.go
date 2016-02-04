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
	"io/ioutil"
	"net/http"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/client"
	. "github.com/ubuntu-core/snappy/cmd/snap"
)

func (s *SnapSuite) TestSkillsHelp(c *C) {
	msg := `Usage:
  snap.test [OPTIONS] skills [skills-OPTIONS] [<snap>:<skill>]

This command skills in the system.

By default all skills, used and offered by all snaps are displayed.

Skills used and offered by a particular snap can be listed with: snap skills
<snap name>

Help Options:
  -h, --help                Show this help message

[skills command options]
          --type=           constrain listing to skills of this type
`
	rest, err := Parser().ParseArgs([]string{"skills", "--help"})
	c.Assert(err.Error(), Equals, msg)
	c.Assert(rest, DeepEquals, []string{})
}

func (s *SnapSuite) TestSkillsSmoke(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/2.0/skills")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(w, c, map[string]interface{}{
			"type": "sync",
			"result": []client.SkillGrants{
				{
					Skill: client.Skill{
						Snap:  "canonical-pi2",
						Name:  "pin-13",
						Type:  "bool-file",
						Label: "Pin 13",
					},
					GrantedTo: []client.Slot{
						{
							Snap: "keyboard-lights",
							Name: "capslock-led",
						},
					},
				},
			},
		})
	})
	rest, err := Parser().ParseArgs([]string{"skills"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
}
