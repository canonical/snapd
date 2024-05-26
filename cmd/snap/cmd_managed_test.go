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

	"github.com/ddkwork/golibrary/mylog"
	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestManaged(c *C) {
	for _, managed := range []bool{true, false} {
		s.stdout.Truncate(0)

		s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
			c.Check(r.Method, Equals, "GET")
			c.Check(r.URL.Path, Equals, "/v2/system-info")

			fmt.Fprintf(w, `{"type":"sync", "status-code": 200, "result": {"managed":%v}}`, managed)
		})

		_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"managed"}))

		c.Check(s.Stdout(), Equals, fmt.Sprintf("%v\n", managed))
	}
}
