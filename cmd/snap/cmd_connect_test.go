// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !integrationcoverage

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

	. "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestConnectHelp(c *C) {
	msg := `Usage:
  snap.test [OPTIONS] connect <snap>:<plug> <snap>:<slot>

The connect command connects a plug to a slot.
It may be called in the following ways:

$ snap connect <snap>:<plug> <snap>:<slot>

Connects the specific plug to the specific slot.

$ snap connect <snap>:<plug> <snap>

Connects the specific plug to the only slot in the provided snap that matches
the connected interface. If more than one potential slot exists, the command
fails.

$ snap connect <plug> <snap>[:<slot>]

Without a name for the snap offering the plug, the plug name is looked at in
the gadget snap, the kernel snap, and then the os snap, in that order. The
first of these snaps that has a matching plug name is used and the command
proceeds as above.

Application Options:
      --version            print the version and exit

Help Options:
  -h, --help               Show this help message
`
	rest, err := Parser().ParseArgs([]string{"connect", "--help"})
	c.Assert(err.Error(), Equals, msg)
	c.Assert(rest, DeepEquals, []string{})
}

func (s *SnapSuite) TestConnectExplicitEverything(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/interfaces":
			c.Check(r.Method, Equals, "POST")
			c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
				"action": "connect",
				"plugs": []interface{}{
					map[string]interface{}{
						"snap": "producer",
						"plug": "plug",
					},
				},
				"slots": []interface{}{
					map[string]interface{}{
						"snap": "consumer",
						"slot": "slot",
					},
				},
			})
			fmt.Fprintln(w, `{"type":"async", "status-code": 202, "change": "zzz"}`)
		case "/v2/changes/zzz":
			c.Check(r.Method, Equals, "GET")
			fmt.Fprintln(w, `{"type":"sync", "result":{"ready": true, "status": "Done"}}`)
		default:
			c.Fatalf("unexpected path %q", r.URL.Path)
		}
	})
	rest, err := Parser().ParseArgs([]string{"connect", "producer:plug", "consumer:slot"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
}

func (s *SnapSuite) TestConnectExplicitPlugImplicitSlot(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/interfaces":
			c.Check(r.Method, Equals, "POST")
			c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
				"action": "connect",
				"plugs": []interface{}{
					map[string]interface{}{
						"snap": "producer",
						"plug": "plug",
					},
				},
				"slots": []interface{}{
					map[string]interface{}{
						"snap": "consumer",
						"slot": "",
					},
				},
			})
			fmt.Fprintln(w, `{"type":"async", "status-code": 202, "change": "zzz"}`)
		case "/v2/changes/zzz":
			c.Check(r.Method, Equals, "GET")
			fmt.Fprintln(w, `{"type":"sync", "result":{"ready": true, "status": "Done"}}`)
		default:
			c.Fatalf("unexpected path %q", r.URL.Path)
		}
	})
	rest, err := Parser().ParseArgs([]string{"connect", "producer:plug", "consumer"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
}

func (s *SnapSuite) TestConnectImplicitPlugExplicitSlot(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/interfaces":
			c.Check(r.Method, Equals, "POST")
			c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
				"action": "connect",
				"plugs": []interface{}{
					map[string]interface{}{
						"snap": "",
						"plug": "plug",
					},
				},
				"slots": []interface{}{
					map[string]interface{}{
						"snap": "consumer",
						"slot": "slot",
					},
				},
			})
			fmt.Fprintln(w, `{"type":"async", "status-code": 202, "change": "zzz"}`)
		case "/v2/changes/zzz":
			c.Check(r.Method, Equals, "GET")
			fmt.Fprintln(w, `{"type":"sync", "result":{"ready": true, "status": "Done"}}`)
		default:
			c.Fatalf("unexpected path %q", r.URL.Path)
		}
	})
	rest, err := Parser().ParseArgs([]string{"connect", "plug", "consumer:slot"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
}

func (s *SnapSuite) TestConnectImplicitPlugImplicitSlot(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/interfaces":
			c.Check(r.Method, Equals, "POST")
			c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
				"action": "connect",
				"plugs": []interface{}{
					map[string]interface{}{
						"snap": "",
						"plug": "plug",
					},
				},
				"slots": []interface{}{
					map[string]interface{}{
						"snap": "consumer",
						"slot": "",
					},
				},
			})
			fmt.Fprintln(w, `{"type":"async", "status-code": 202, "change": "zzz"}`)
		case "/v2/changes/zzz":
			c.Check(r.Method, Equals, "GET")
			fmt.Fprintln(w, `{"type":"sync", "result":{"ready": true, "status": "Done"}}`)
		default:
			c.Fatalf("unexpected path %q", r.URL.Path)
		}
	})
	rest, err := Parser().ParseArgs([]string{"connect", "plug", "consumer"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
}
