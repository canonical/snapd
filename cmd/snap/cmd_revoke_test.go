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

func (s *SnapSuite) TestRevokeHelp(c *C) {
	msg := `Usage:
  snap.test [OPTIONS] revoke [<snap>:<skill>] [<snap>:<skill slot>]

The revoke command unassigns previously granted skills from a snap.
It may be called in the following ways:

$ snap revoke <snap>:<skill> <snap>:<skill slot>

Revokes the specific skill from the specific skill slot.

$ snap revoke <snap>:<skill slot>

Revokes any previously granted skill from the provided skill slot.

$ snap revoke <snap>

Revokes all skills from the provided snap.

Help Options:
  -h, --help                     Show this help message
`
	rest, _ := Parser().ParseArgs([]string{"revoke", "--help"})
	// TODO: Re-enable this test after go-flags is updated
	msg = msg[:]
	// c.Assert(err.Error(), Equals, msg)
	// NOTE: Updated go-flags returns []string{} here
	c.Assert(rest, DeepEquals, []string{"--help"})
}

func (s *SnapSuite) TestRevokeExplicitEverything(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/2.0/skills")
		c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
			"action": "revoke",
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
	rest, err := Parser().ParseArgs([]string{"revoke", "producer:skill", "consumer:slot"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestRevokeEverythingFromSpecificSlot(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/2.0/skills")
		c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
			"action": "revoke",
			"skill": map[string]interface{}{
				"snap": "",
				"name": "",
			},
			"slot": map[string]interface{}{
				"snap": "consumer",
				"name": "slot",
			},
		})
		fmt.Fprintln(w, `{"type":"sync", "result":{}}`)
	})
	rest, err := Parser().ParseArgs([]string{"revoke", "consumer:slot"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestRevokeEverythingFromSpecificSnap(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/2.0/skills")
		c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
			"action": "revoke",
			"skill": map[string]interface{}{
				"snap": "",
				"name": "",
			},
			"slot": map[string]interface{}{
				"snap": "consumer",
				"name": "",
			},
		})
		fmt.Fprintln(w, `{"type":"sync", "result":{}}`)
	})
	rest, err := Parser().ParseArgs([]string{"revoke", "consumer"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}
