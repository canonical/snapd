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
	"net/http"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/osutil"
)

func (s *SnapSuite) TestWhoamiLoggedInUser(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		panic("unexpected call to snapd API")
	})

	s.Login(c)
	defer s.Logout(c)

	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"whoami"}))

	c.Check(s.Stdout(), Equals, "email: hello@mail.com\n")
}

func (s *SnapSuite) TestWhoamiNotLoggedInUser(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		panic("unexpected call to snapd API")
	})

	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"whoami"}))

	c.Check(s.Stdout(), Equals, "email: -\n")
}

func (s *SnapSuite) TestWhoamiExtraParamError(c *C) {
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"whoami", "test"}))
	c.Check(err, ErrorMatches, "too many arguments for command")
}

func (s *SnapSuite) TestWhoamiEmptyAuthFile(c *C) {
	s.Login(c)
	defer s.Logout(c)
	mylog.Check(osutil.AtomicWriteFile(s.AuthFile, []byte(``), 0600, 0))


	_ = mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"whoami"}))
	c.Check(err, ErrorMatches, "EOF")
}
