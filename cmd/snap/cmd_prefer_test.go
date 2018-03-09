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

	. "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestPreferHelp(c *C) {
	msg := `Usage:
  snap.test [OPTIONS] prefer [prefer-OPTIONS] [<snap>]

The prefer command enables all aliases of the given snap in preference
to conflicting aliases of other snaps whose aliases will be disabled
(removed for manual ones).

Application Options:
      --version      Print the version and exit

Help Options:
  -h, --help         Show this help message

[prefer command options]
          --no-wait  Do not wait for the operation to finish but just print the
                     change id.
`
	rest, err := Parser().ParseArgs([]string{"prefer", "--help"})
	c.Assert(err.Error(), Equals, msg)
	c.Assert(rest, DeepEquals, []string{})
}

func (s *SnapSuite) TestPrefer(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/aliases":
			c.Check(r.Method, Equals, "POST")
			c.Check(DecodedRequestBody(c, r), DeepEquals, map[string]interface{}{
				"action": "prefer",
				"snap":   "some-snap",
			})
			fmt.Fprintln(w, `{"type":"async", "status-code": 202, "change": "zzz"}`)
		case "/v2/changes/zzz":
			c.Check(r.Method, Equals, "GET")
			fmt.Fprintln(w, `{"type":"sync", "result":{"ready": true, "status": "Done", "data": {"aliases-added": [{"alias": "alias1", "snap": "some-snap", "app": "cmd1"}]}}}`)
		default:
			c.Fatalf("unexpected path %q", r.URL.Path)
		}
	})
	rest, err := Parser().ParseArgs([]string{"prefer", "some-snap"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Assert(s.Stdout(), Equals, ""+
		"Added:\n"+
		"  - some-snap.cmd1 as alias1\n",
	)
	c.Assert(s.Stderr(), Equals, "")
}
