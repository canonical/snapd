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
	"io"
	"net/http"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestGetBaseDeclaration(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/debug")
			c.Check(r.URL.RawQuery, check.Equals, "aspect=base-declaration")
			data := mylog.Check2(io.ReadAll(r.Body))
			c.Check(err, check.IsNil)
			c.Check(data, check.HasLen, 0)
			fmt.Fprintln(w, `{"type": "sync", "result": {"base-declaration": "hello"}}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"debug", "get-base-declaration"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, "hello\n")
	c.Check(s.Stderr(), check.Equals, `'snap debug get-base-declaration' is deprecated; use 'snap debug base-declaration'.`)
}

func (s *SnapSuite) TestBaseDeclaration(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/debug")
			c.Check(r.URL.RawQuery, check.Equals, "aspect=base-declaration")
			data := mylog.Check2(io.ReadAll(r.Body))
			c.Check(err, check.IsNil)
			c.Check(data, check.HasLen, 0)
			fmt.Fprintln(w, `{"type": "sync", "result": {"base-declaration": "hello"}}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"debug", "base-declaration"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, "hello\n")
	c.Check(s.Stderr(), check.Equals, "")
}
