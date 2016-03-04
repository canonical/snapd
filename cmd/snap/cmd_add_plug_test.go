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

func (s *SnapSuite) TestAddInterfaceHelp(c *C) {
	msg := `Usage:
  snap.test [OPTIONS] experimental add-plug [add-plug-OPTIONS] <snap> <plug> <interface>

The add-plug command adds a new plug to the system.

This command is only for experimentation with interfaces.
It will be removed in one of the future releases.

Help Options:
  -h, --help             Show this help message

[add-plug command options]
      -a=                List of key=value attributes
          --app=         List of apps providing this plug
          --label=       Human-friendly label

[add-plug command arguments]
  <snap>:                Name of the snap offering the interface
  <plug>:                Plug name within the snap
  <interface>:           Interface name
`
	rest, err := Parser().ParseArgs([]string{"experimental", "add-plug", "--help"})
	c.Assert(err.Error(), Equals, msg)
	c.Assert(rest, DeepEquals, []string{})
}

func (s *SnapSuite) TestAddInterfaceExplicitEverything(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/2.0/interfaces")
		c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
			"action": "add-plug",
			"plugs": []interface{}{
				map[string]interface{}{
					"snap":      "producer",
					"plug":      "plug",
					"interface": "interface",
					"attrs": map[string]interface{}{
						"attr": "value",
					},
					"apps": []interface{}{
						"meta/hooks/interfaces",
					},
					"label": "label",
				},
			},
		})
		fmt.Fprintln(w, `{"type":"sync", "result":{}}`)
	})
	rest, err := Parser().ParseArgs([]string{
		"experimental", "add-plug", "producer", "plug", "interface",
		"-a", "attr=value", "--app=meta/hooks/interfaces", "--label=label",
	})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}
