// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestPortalInfo(c *C) {
	snap.MockSnapNameFromPid(func(pid int) (string, error) {
		c.Check(pid, Equals, 42)
		return "hello", nil
	})
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/v2/snaps/hello")
		fmt.Fprintln(w, mockInfoJSONNoLicense)
	})
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"routine", "portal-info", "42"})
	c.Assert(err, IsNil)
	c.Check(s.Stdout(), Equals, `[Snap Info]
InstanceName=hello
`)
	c.Check(s.Stderr(), Equals, "")
}
