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

	snap "github.com/snapcore/snapd/cmd/snap"
)

var managedTests = []struct {
	managed, quiet bool
	stdout         string
}{{
	managed: true,
	quiet:   false,
	stdout:  "system is managed\n",
}, {
	managed: false,
	quiet:   false,
	stdout:  "system is not managed\n",
}, {
	managed: true,
	quiet:   true,
}, {
	managed: false,
	quiet:   true,
}}

func (s *SnapSuite) TestManagedTrue(c *C) {
	for _, test := range managedTests {
		c.Logf("Test: %#v", test)

		s.stdout.Truncate(0)

		s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/system-info")

			fmt.Fprintf(w, `{"type":"sync", "status-code": 200, "result": {"managed":%v}}`, test.managed)
		})

		args := []string{"managed"}
		if test.quiet {
			args = append(args, "-q")
		}
		f := func() { snap.Parser().ParseArgs(args) }

		if test.managed {
			c.Assert(f, PanicMatches, ".*exitStatus{0}.*")
		} else {
			c.Assert(f, PanicMatches, ".*exitStatus{1}.*")
		}

		c.Check(s.Stdout(), Equals, test.stdout)
	}
}
