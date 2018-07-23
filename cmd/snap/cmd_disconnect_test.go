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
	"os"

	"github.com/jessevdk/go-flags"
	. "gopkg.in/check.v1"

	. "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestDisconnectHelp(c *C) {
	msg := `Usage:
  snap.test disconnect [disconnect-OPTIONS] [<snap>:<plug>] [<snap>:<slot>]

The disconnect command disconnects a plug from a slot.
It may be called in the following ways:

$ snap disconnect <snap>:<plug> <snap>:<slot>

Disconnects the specific plug from the specific slot.

$ snap disconnect <snap>:<slot or plug>

Disconnects everything from the provided plug or slot.
The snap name may be omitted for the core snap.

[disconnect command options]
          --no-wait        Do not wait for the operation to finish but just
                           print the change id.
`
	rest, err := Parser().ParseArgs([]string{"disconnect", "--help"})
	c.Assert(err.Error(), Equals, msg)
	c.Assert(rest, DeepEquals, []string{})
}

func (s *SnapSuite) TestDisconnectExplicitEverything(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/interfaces":
			c.Check(r.Method, Equals, "POST")
			c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
				"action": "disconnect",
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
	rest, err := Parser().ParseArgs([]string{"disconnect", "producer:plug", "consumer:slot"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestDisconnectEverythingFromSpecificSlot(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/interfaces":
			c.Check(r.Method, Equals, "POST")
			c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
				"action": "disconnect",
				"plugs": []interface{}{
					map[string]interface{}{
						"snap": "",
						"plug": "",
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
	rest, err := Parser().ParseArgs([]string{"disconnect", "consumer:slot"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestDisconnectEverythingFromSpecificSnapPlugOrSlot(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/interfaces":
			c.Check(r.Method, Equals, "POST")
			c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
				"action": "disconnect",
				"plugs": []interface{}{
					map[string]interface{}{
						"snap": "",
						"plug": "",
					},
				},
				"slots": []interface{}{
					map[string]interface{}{
						"snap": "consumer",
						"slot": "plug-or-slot",
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
	rest, err := Parser().ParseArgs([]string{"disconnect", "consumer:plug-or-slot"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestDisconnectEverythingFromSpecificSnap(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Fatalf("expected nothing to reach the server")
	})
	rest, err := Parser().ParseArgs([]string{"disconnect", "consumer"})
	c.Assert(err, ErrorMatches, `please provide the plug or slot name to disconnect from snap "consumer"`)
	c.Assert(rest, DeepEquals, []string{"consumer"})
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestDisconnectCompletion(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/interfaces":
			c.Assert(r.Method, Equals, "GET")
			EncodeResponseBody(c, w, map[string]interface{}{
				"type":   "sync",
				"result": fortestingConnectionList,
			})
		default:
			c.Fatalf("unexpected path %q", r.URL.Path)
		}
	})
	os.Setenv("GO_FLAGS_COMPLETION", "verbose")
	defer os.Unsetenv("GO_FLAGS_COMPLETION")

	expected := []flags.Completion{}
	parser := Parser()
	parser.CompletionHandler = func(obtained []flags.Completion) {
		c.Check(obtained, DeepEquals, expected)
	}

	expected = []flags.Completion{{Item: "canonical-pi2:"}, {Item: "core:"}, {Item: "keyboard-lights:"}}
	_, err := parser.ParseArgs([]string{"disconnect", ""})
	c.Assert(err, IsNil)

	expected = []flags.Completion{{Item: "canonical-pi2:pin-13", Description: "slot"}}
	_, err = parser.ParseArgs([]string{"disconnect", "ca"})
	c.Assert(err, IsNil)

	expected = []flags.Completion{{Item: ":core-support", Description: "slot"}, {Item: ":core-support-plug", Description: "plug"}}
	_, err = parser.ParseArgs([]string{"disconnect", ":"})
	c.Assert(err, IsNil)

	expected = []flags.Completion{{Item: "keyboard-lights:capslock-led", Description: "plug"}}
	_, err = parser.ParseArgs([]string{"disconnect", "k"})
	c.Assert(err, IsNil)

	expected = []flags.Completion{{Item: "canonical-pi2:"}, {Item: "core:"}}
	_, err = parser.ParseArgs([]string{"disconnect", "keyboard-lights:capslock-led", ""})
	c.Assert(err, IsNil)

	expected = []flags.Completion{{Item: "canonical-pi2:pin-13", Description: "slot"}}
	_, err = parser.ParseArgs([]string{"disconnect", "keyboard-lights:capslock-led", "ca"})
	c.Assert(err, IsNil)

	expected = []flags.Completion{{Item: ":core-support", Description: "slot"}}
	_, err = parser.ParseArgs([]string{"disconnect", ":core-support-plug", ":"})
	c.Assert(err, IsNil)

	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}
