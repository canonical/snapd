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

func (s *SnapSuite) TestGrantHelp(c *C) {
	msg := `Usage:
  snap.test [OPTIONS] grant <snap>:<skill> <snap>:<skill slot>

The grant command assigns a skill to a snap.
It may be called in the following ways:

$ snap grant <snap>:<skill> <snap>:<skill slot>

Grants the specific skill to the specific skill slot.

$ snap grant <snap>:<skill> <snap>

Grants the specific skill to the only skill slot in the provided snap that
matches the granted skill type. If more than one potential slot exists, the
command fails.

$ snap grant <skill> <snap>[:<skill slot>]

Without a name for the snap offering the skill, the skill name is looked at in
the gadget snap, the kernel snap, and then the os snap, in that order. The
first of these snaps that has a matching skill name is used and the command
proceeds as above.

Help Options:
  -h, --help                     Show this help message
`
	rest, _ := Parser().ParseArgs([]string{"grant", "--help"})
	// TODO: re-enable this once go-flags is updated
	msg = msg[:]
	// c.Assert(err.Error(), Equals, msg)
	c.Assert(rest, DeepEquals, []string{"--help"})
}

func (s *SnapSuite) TestGrantExplicitEverything(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/2.0/skills")
		c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
			"action": "grant",
			"skill": map[string]interface{}{
				"snap": "producer",
				"name": "skill",
			},
			"slot": map[string]interface{}{
				"snap": "consumer",
				"name": "slot",
			},
		})
		fmt.Fprintln(w, `{"type":"sync", "result":{}}`)
	})
	rest, err := Parser().ParseArgs([]string{"grant", "producer:skill", "consumer:slot"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
}

func (s *SnapSuite) TestGrantExplicitSkillImplicitSlot(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/2.0/skills")
		c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
			"action": "grant",
			"skill": map[string]interface{}{
				"snap": "producer",
				"name": "skill",
			},
			"slot": map[string]interface{}{
				"snap": "consumer",
				"name": "",
			},
		})
		fmt.Fprintln(w, `{"type":"sync", "result":{}}`)
	})
	rest, err := Parser().ParseArgs([]string{"grant", "producer:skill", "consumer"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
}

func (s *SnapSuite) TestGrantImplicitSkillExplicitSlot(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/2.0/skills")
		c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
			"action": "grant",
			"skill": map[string]interface{}{
				"snap": "",
				"name": "skill",
			},
			"slot": map[string]interface{}{
				"snap": "consumer",
				"name": "slot",
			},
		})
		fmt.Fprintln(w, `{"type":"sync", "result":{}}`)
	})
	rest, err := Parser().ParseArgs([]string{"grant", "skill", "consumer:slot"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
}

func (s *SnapSuite) TestGrantImplicitSkillImplicitSlot(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/2.0/skills")
		c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
			"action": "grant",
			"skill": map[string]interface{}{
				"snap": "",
				"name": "skill",
			},
			"slot": map[string]interface{}{
				"snap": "consumer",
				"name": "",
			},
		})
		fmt.Fprintln(w, `{"type":"sync", "result":{}}`)
	})
	rest, err := Parser().ParseArgs([]string{"grant", "skill", "consumer"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
}
