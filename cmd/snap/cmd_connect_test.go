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

func (s *SnapSuite) TestConnectHelp(c *C) {
	msg := `Usage:
  snap.test [OPTIONS] connect <snap>:<slot> <snap>:<plug>

The connect command connects a slot to a plug.
It may be called in the following ways:

$ snap connect <snap>:<slot> <snap>:<plug>

Connects the specific slot to the specific plug.

$ snap connect <snap>:<slot> <snap>

Connects the specific slot to the only plug in the provided snap that matches
the connected interface. If more than one potential plug exists, the command
fails.

$ snap connect <slot> <snap>[:<plug>]

Without a name for the snap offering the slot, the slot name is looked at in
the gadget snap, the kernel snap, and then the os snap, in that order. The
first of these snaps that has a matching slot name is used and the command
proceeds as above.

Help Options:
  -h, --help               Show this help message
`
	rest, err := Parser().ParseArgs([]string{"connect", "--help"})
	c.Assert(err.Error(), Equals, msg)
	c.Assert(rest, DeepEquals, []string{})
}

func (s *SnapSuite) TestConnectExplicitEverything(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/2.0/interfaces")
		c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
			"action": "connect",
			"slot": map[string]interface{}{
				"snap": "producer",
				"slot": "slot",
			},
			"plug": map[string]interface{}{
				"snap": "consumer",
				"plug": "plug",
			},
		})
		fmt.Fprintln(w, `{"type":"sync", "result":{}}`)
	})
	rest, err := Parser().ParseArgs([]string{"connect", "producer:slot", "consumer:plug"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
}

func (s *SnapSuite) TestConnectExplicitSlotImplicitPlug(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/2.0/interfaces")
		c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
			"action": "connect",
			"slot": map[string]interface{}{
				"snap": "producer",
				"slot": "slot",
			},
			"plug": map[string]interface{}{
				"snap": "consumer",
				"plug": "",
			},
		})
		fmt.Fprintln(w, `{"type":"sync", "result":{}}`)
	})
	rest, err := Parser().ParseArgs([]string{"connect", "producer:slot", "consumer"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
}

func (s *SnapSuite) TestConnectImplicitSlotExplicitPlug(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/2.0/interfaces")
		c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
			"action": "connect",
			"slot": map[string]interface{}{
				"snap": "",
				"slot": "slot",
			},
			"plug": map[string]interface{}{
				"snap": "consumer",
				"plug": "plug",
			},
		})
		fmt.Fprintln(w, `{"type":"sync", "result":{}}`)
	})
	rest, err := Parser().ParseArgs([]string{"connect", "slot", "consumer:plug"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
}

func (s *SnapSuite) TestConnectImplicitSlotImplicitPlug(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/2.0/interfaces")
		c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
			"action": "connect",
			"slot": map[string]interface{}{
				"snap": "",
				"slot": "slot",
			},
			"plug": map[string]interface{}{
				"snap": "consumer",
				"plug": "",
			},
		})
		fmt.Fprintln(w, `{"type":"sync", "result":{}}`)
	})
	rest, err := Parser().ParseArgs([]string{"connect", "slot", "consumer"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
}
