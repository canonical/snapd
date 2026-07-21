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

package cli_test

import (
	"io"
	"net/http"
	"os"

	"github.com/jessevdk/go-flags"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	. "github.com/snapcore/snapd/cmd/snapd/cli"
)

func (s *SnapSuite) TestInterfaceHelp(c *C) {
	msg := `Usage:
  cli.test interface [interface-OPTIONS] [<interface>]

The interface command shows details of snap interfaces.

If no interface name is provided, a list of interface names with at least
one connection is shown, or a list of all interfaces if --all is provided.

[interface command options]
      --attrs          Show interface attributes
      --all            Include unused interfaces

[interface command arguments]
  <interface>:         Show details of a specific interface
`
	s.testSubCommandHelp(c, "interface", msg)
}

func (s *SnapSuite) TestInterfaceListEmpty(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interfaces")
		c.Check(r.URL.RawQuery, Equals, "select=connected")
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]any{
			"type":   "sync",
			"result": []*client.Interface{},
		})
	})
	rest, err := Parser(Client()).ParseArgs([]string{"interface"})
	c.Assert(err, ErrorMatches, "no interfaces currently connected")
	c.Assert(rest, DeepEquals, []string{"interface"})
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestInterfaceListAllEmpty(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interfaces")
		c.Check(r.URL.RawQuery, Equals, "select=all")
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]any{
			"type":   "sync",
			"result": []*client.Interface{},
		})
	})
	rest, err := Parser(Client()).ParseArgs([]string{"interface", "--all"})
	c.Assert(err, ErrorMatches, "no interfaces found")
	c.Assert(rest, DeepEquals, []string{"--all"}) // XXX: feels like a bug in go-flags.
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestInterfaceList(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interfaces")
		c.Check(r.URL.RawQuery, Equals, "select=connected")
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]any{
			"type": "sync",
			"result": []*client.Interface{{
				Name:    "network",
				Summary: "allows access to the network",
			}, {
				Name:    "network-bind",
				Summary: "allows providing services on the network",
			}},
		})
	})
	rest, err := Parser(Client()).ParseArgs([]string{"interface"})
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
		c.Check(r.URL.Path, Equals, "/v2/interfaces")
		c.Check(r.URL.RawQuery, Equals, "select=all")
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]any{
			"type": "sync",
			"result": []*client.Interface{{
				Name:    "network",
				Summary: "allows access to the network",
			}, {
				Name:    "network-bind",
				Summary: "allows providing services on the network",
			}, {
				Name:    "unused",
				Summary: "just an unused interface, nothing to see here",
			}},
		})
	})
	rest, err := Parser(Client()).ParseArgs([]string{"interface", "--all"})
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
		c.Check(r.URL.Path, Equals, "/v2/interfaces")
		c.Check(r.URL.RawQuery, Equals, "doc=true&names=network&plugs=true&select=all&slots=true")
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]any{
			"type": "sync",
			"result": []*client.Interface{{
				Name:    "network",
				Summary: "allows access to the network",
				DocURL:  "http://example.org/about-the-network-interface",
				Plugs: []client.Plug{
					{Snap: "deepin-music", Name: "network"},
					{Snap: "http", Name: "network"},
				},
				Slots: []client.Slot{{Snap: "system", Name: "network"}},
			}},
		})
	})
	rest, err := Parser(Client()).ParseArgs([]string{"interface", "network"})
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
		"  - system\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestInterfaceDetailsAndAttrs(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interfaces")
		c.Check(r.URL.RawQuery, Equals, "doc=true&names=serial-port&plugs=true&select=all&slots=true")
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]any{
			"type": "sync",
			"result": []*client.Interface{{
				Name:    "serial-port",
				Summary: "allows providing or using a specific serial port",
				Plugs: []client.Plug{
					{Snap: "minicom", Name: "serial-port"},
				},
				Slots: []client.Slot{{
					Snap:  "gizmo-gadget",
					Name:  "debug-serial-port",
					Label: "serial port for debugging",
					Attrs: map[string]any{
						"header":   "pin-array",
						"location": "internal",
						"path":     "/dev/ttyS0",
						"number":   1,
					},
				}},
			}},
		})
	})
	rest, err := Parser(Client()).ParseArgs([]string{"interface", "--attrs", "serial-port"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"name:    serial-port\n" +
		"summary: allows providing or using a specific serial port\n" +
		"plugs:\n" +
		"  - minicom\n" +
		"slots:\n" +
		"  - gizmo-gadget:debug-serial-port (serial port for debugging):\n" +
		"      header:   pin-array\n" +
		"      location: internal\n" +
		"      number:   1\n" +
		"      path:     /dev/ttyS0\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestInterfaceDetailsAndAttrsComplex(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interfaces")
		c.Check(r.URL.RawQuery, Equals, "doc=true&names=system-files&plugs=true&select=all&slots=true")
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(body, DeepEquals, []byte{})
		EncodeResponseBody(c, w, map[string]any{
			"type": "sync",
			"result": []*client.Interface{{
				Name:    "system-files",
				Summary: "allows access to system files or directories",
				Plugs: []client.Plug{{
					Snap: "my-app",
					Name: "system-files",
					Attrs: map[string]any{
						"read":  []any{"/etc/foo", "/etc/bar"},
						"write": []any{"/var/lib/baz"},
					},
				}},
			}},
		})
	})
	rest, err := Parser(Client()).ParseArgs([]string{"interface", "--attrs", "system-files"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedStdout := "" +
		"name:    system-files\n" +
		"summary: allows access to system files or directories\n" +
		"plugs:\n" +
		"  - my-app:\n" +
		"      read:\n" +
		"        - /etc/foo\n" +
		"        - /etc/bar\n" +
		"      write:\n" +
		"        - /var/lib/baz\n"
	c.Assert(s.Stdout(), Equals, expectedStdout)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestInterfaceCompletion(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/interfaces")
		c.Check(r.URL.RawQuery, Equals, "select=all")
		EncodeResponseBody(c, w, map[string]any{
			"type": "sync",
			"result": []*client.Interface{{
				Name:    "network",
				Summary: "allows access to the network",
			}, {
				Name:    "network-bind",
				Summary: "allows providing services on the network",
			}},
		})
	})
	os.Setenv("GO_FLAGS_COMPLETION", "verbose")
	defer os.Unsetenv("GO_FLAGS_COMPLETION")

	expected := []flags.Completion{}
	parser := Parser(Client())
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
