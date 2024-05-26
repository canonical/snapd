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
	"io"
	"net/http"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	snap "github.com/snapcore/snapd/cmd/snap"
)

var mockLoginRsp = `{"type": "sync", "result": {"id":42, "username": "foo", "email": "foo@example.com", "macaroon": "yummy", "discarages":"plenty"}}`

func makeLoginTestServer(c *C, n *int) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		switch *n {
		case 0:
			c.Check(r.URL.Path, Equals, "/v2/login")
			c.Check(r.Method, Equals, "POST")
			postData := mylog.Check2(io.ReadAll(r.Body))

			c.Check(string(postData), Equals, `{"email":"foo@example.com","password":"some-password"}`+"\n")
			fmt.Fprintln(w, mockLoginRsp)
		default:
			c.Fatalf("unexpected path %q", r.URL.Path)
		}
		*n++
	}
}

func (s *SnapSuite) TestLoginSimple(c *C) {
	n := 0
	s.RedirectClientToTestServer(makeLoginTestServer(c, &n))

	// send the password
	s.password = "some-password\n"
	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"login", "foo@example.com"}))

	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, `Personal information is handled as per our privacy notice at
https://www.ubuntu.com/legal/dataprivacy/snap-store

Password of "foo@example.com": 
Login successful
`)
	c.Check(s.Stderr(), Equals, "")
	c.Check(n, Equals, 1)
}

func (s *SnapSuite) TestLoginAskEmail(c *C) {
	n := 0
	s.RedirectClientToTestServer(makeLoginTestServer(c, &n))

	// send the email
	fmt.Fprint(s.stdin, "foo@example.com\n")
	// send the password
	s.password = "some-password"

	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"login"}))

	c.Assert(rest, DeepEquals, []string{})
	// test slightly ugly, on a real system STDOUT will be:
	//    Email address: foo@example.com\n
	// because the input to stdin is echoed
	c.Check(s.Stdout(), Equals, `Personal information is handled as per our privacy notice at
https://www.ubuntu.com/legal/dataprivacy/snap-store

Email address: Password of "foo@example.com": 
Login successful
`)
	c.Check(s.Stderr(), Equals, "")
	c.Check(n, Equals, 1)
}
