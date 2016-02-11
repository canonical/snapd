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

func (s *SnapSuite) TestRemoveSkillSlotHelp(c *C) {
	msg := `Usage:
  snap.test [OPTIONS] experimental remove-skill-slot <snap> <skill slot>

The remove-skill-slot command removes a skill slot from the system.

This command is only for experimentation with the skill system.
It will be removed in one of the future releases.

Help Options:
  -h, --help              Show this help message

[remove-skill-slot command arguments]
  <snap>:                 Name of the snap containing the skill slot
  <skill slot>:           Name of the skill slot within the snap
`
	rest, _ := Parser().ParseArgs([]string{"experimental", "remove-skill-slot", "--help"})
	// TODO: Re-enable this test after go-flags is updated
	msg = msg[:]
	// c.Assert(err.Error(), Equals, msg)
	// NOTE: Updated go-flags returns []string{} here
	c.Assert(rest, DeepEquals, []string{"--help"})
}

func (s *SnapSuite) TestRemoveSkillSlot(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/2.0/skills")
		c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
			"action": "remove-slot",
			"slot": map[string]interface{}{
				"snap": "consumer",
				"name": "slot",
			},
		})
		fmt.Fprintln(w, `{"type":"sync", "result":{}}`)
	})
	rest, err := Parser().ParseArgs([]string{
		"experimental", "remove-skill-slot", "consumer", "slot",
	})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}
