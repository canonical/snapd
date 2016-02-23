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

func (s *SnapSuite) TestAddSlotHelp(c *C) {
	msg := `Usage:
  snap.test [OPTIONS] experimental add-slot [add-slot-OPTIONS] <snap> <slot> <interface>

The add-slot command adds a new slot to the system.

This command is only for experimentation with interfaces.
It will be removed in one of the future releases.

Help Options:
  -h, --help             Show this help message

[add-slot command options]
      -a=                List of key=value attributes
          --app=         List of apps using this slot
          --label=       Human-friendly label

[add-slot command arguments]
  <snap>:                Name of the snap containing the slot
  <slot>:                Name of the slot within the snap
  <interface>:           Interface name
`
	rest, err := Parser().ParseArgs([]string{"experimental", "add-slot", "--help"})
	c.Assert(err.Error(), Equals, msg)
	c.Assert(rest, DeepEquals, []string{})
}

func (s *SnapSuite) TestAddSlotExplicitEverything(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/2.0/skills")
		c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
			"action": "add-slot",
			"slot": map[string]interface{}{
				"snap": "consumer",
				"name": "slot",
				"type": "interface",
				"attrs": map[string]interface{}{
					"attr": "value",
				},
				"apps": []interface{}{
					"my-app",
				},
				"label": "label",
			},
		})
		fmt.Fprintln(w, `{"type":"sync", "result":{}}`)
	})
	rest, err := Parser().ParseArgs([]string{
		"experimental", "add-slot", "consumer", "slot", "interface",
		"-a", "attr=value", "--app=my-app", "--label=label",
	})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}
