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

func (s *SnapSuite) TestRemoveSkillHelp(c *C) {
	msg := `Usage:
  snap.test [OPTIONS] experimental remove-skill <snap> <skill>

The remove-skill command removes a skill from the system.

This command is only for experimentation with the skill system.
It will be removed in one of the future releases.

Help Options:
  -h, --help         Show this help message

[remove-skill command arguments]
  <snap>:            Name of the snap containing the skill
  <skill>:           Name of the skill slot within the snap
`
	rest, _ := Parser().ParseArgs([]string{
		"experimental", "remove-skill", "--help"})
	msg = msg[:]
	// TODO: Re-enable this test after go-flags is updated
	// c.Assert(err.Error(), Equals, msg)
	// NOTE: Updated go-flags returns []string{} here
	c.Assert(rest, DeepEquals, []string{"--help"})
}

func (s *SnapSuite) TestRemoveSkill(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/2.0/skills")
		c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
			"action": "remove-skill",
			"skill": map[string]interface{}{
				"snap": "producer",
				"name": "skill",
			},
		})
		fmt.Fprintln(w, `{"type":"sync", "result":{}}`)
	})
	rest, err := Parser().ParseArgs([]string{
		"experimental", "remove-skill", "producer", "skill",
	})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}
