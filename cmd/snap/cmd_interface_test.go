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
	"os"

	"github.com/jessevdk/go-flags"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	. "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestInterfaceHelp(c *C) {
	msg := `Usage:
  snap.test [OPTIONS] interface [interface-OPTIONS] [<interface>]

The interface command shows details of snap interfaces.

If no interface name is provided, a list of interface names with at least
one connection is shown, or a list of all interfaces if --all is provided.

Application Options:
      --version          Print the version and exit

Help Options:
  -h, --help             Show this help message

[interface command options]
          --attrs        Show interface attributes
          --all          Include unused interfaces

[interface command arguments]
  <interface>:           Show details of a specific interface
`
	rest, err := Parser().ParseArgs([]string{"interface", "--help"})
	c.Assert(err.Error(), Equals, msg)
	c.Assert(rest, DeepEquals, []string{})
}

func (s *SnapSuite) TestInterfaceList(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interface")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": []client.Interface{{
				Name:    "network",
				Summary: "allows access to the network",
				Used:    true,
			}, {
				Name:    "network-bind",
				Summary: "allows providing services on the network",
				Used:    true,
			}, {
				Name:    "unused",
				Summary: "just an unused interface, nothing to see here",
			}},
		})
	})
	rest, err := Parser().ParseArgs([]string{"interface"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Name          Summary\n" +
		"network       allows access to the network\n" +
		"network-bind  allows providing services on the network\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestInterfaceListAll(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interface")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": []client.Interface{{
				Name:    "network",
				Summary: "allows access to the network",
				Used:    true,
			}, {
				Name:    "network-bind",
				Summary: "allows providing services on the network",
				Used:    true,
			}, {
				Name:    "unused",
				Summary: "just an unused interface, nothing to see here",
			}},
		})
	})
	rest, err := Parser().ParseArgs([]string{"interface", "--all"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"Name          Summary\n" +
		"network       allows access to the network\n" +
		"network-bind  allows providing services on the network\n" +
		"unused        just an unused interface, nothing to see here\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestInterfaceDetails(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interface/network")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Interface{
				Name:    "network",
				Summary: "allows access to the network",
				DocsURL: "http://example.org/about-the-network-interface",
				Plugs: []client.Plug{
					{Snap: "deepin-music", Name: "network"},
					{Snap: "http", Name: "network"},
				},
				Slots: []client.Slot{{Snap: "core", Name: "network"}},
			},
		})
	})
	rest, err := Parser().ParseArgs([]string{"interface", "network"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"name:          network\n" +
		"summary:       allows access to the network\n" +
		"documentation: http://example.org/about-the-network-interface\n" +
		"plugs:\n" +
		"  - deepin-music\n" +
		"  - http\n" +
		"slots:\n" +
		"  - core\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestInterfaceDetailsAndAttrs(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interface/serial-port")
		body, err := ioutil.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]interface{}{
			"type": "sync",
			"result": client.Interface{
				Name:    "serial-port",
				Summary: "allows providing or using a specific serial port",
				Plugs: []client.Plug{
					{Snap: "minicom", Name: "serial-port"},
				},
				Slots: []client.Slot{{
					Snap:  "gizmo-gadget",
					Name:  "debug-serial-port",
					Label: "serial port for debugging",
					Attrs: map[string]interface{}{
						"header":   "pin-array",
						"location": "internal",
						"path":     "/dev/ttyS0",
					},
				}},
			},
		})
	})
	rest, err := Parser().ParseArgs([]string{"interface", "--attrs", "serial-port"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"name:    serial-port\n" +
		"summary: allows providing or using a specific serial port\n" +
		"plugs:\n" +
		"  - minicom\n" +
		"slots:\n" +
		"  - \"gizmo-gadget:debug-serial-port\":\n" +
		"      label: serial port for debugging\n" +
		"      attributes:\n" +
		"        header:   pin-array\n" +
		"        location: internal\n" +
		"        path:     /dev/ttyS0\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestInterfaceCompletion(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/interface":
			c.Assert(r.Method, Equals, "GET")
			EncodeResponseBody(c, w, map[string]interface{}{
				"type": "sync",
				"result": []client.Interface{{
					Name:    "network",
					Summary: "allows access to the network",
					Used:    true,
				}, {
					Name:    "network-bind",
					Summary: "allows providing services on the network",
					Used:    true,
				}, {
					Name: "unused",
				}},
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

	expected = []flags.Completion{
		{Item: "network", Description: "allows access to the network"},
		{Item: "network-bind", Description: "allows providing services on the network"},
	}
	_, err := parser.ParseArgs([]string{"interface", ""})
	c.Assert(err, IsNil)

	expected = []flags.Completion{
		{Item: "network-bind", Description: "allows providing services on the network"},
	}
	_, err = parser.ParseArgs([]string{"interface", "network-"})
	c.Assert(err, IsNil)

	expected = []flags.Completion{}
	_, err = parser.ParseArgs([]string{"interface", "bogus"})
	c.Assert(err, IsNil)

	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}
