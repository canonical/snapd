// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

func (s *SnapSuite) TestConfinement(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "sync", "result": {"confinement": "strict"}}`)
	})
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"debug", "confinement"}))

	c.Assert(s.Stdout(), Equals, "strict\n")
	c.Assert(s.Stderr(), Equals, "")
}
