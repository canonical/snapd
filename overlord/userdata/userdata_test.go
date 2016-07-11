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

package userdata_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/userdata"
)

func TestOverlord(t *testing.T) { TestingT(t) }

type userDataSuite struct {
	ud *userdata.UserData
	u  *auth.UserState
	s  *state.State
}

var _ = Suite(&userDataSuite{})

const userID = 7

func (s *userDataSuite) SetUpTest(c *C) {
	s.u = &auth.UserState{ID: userID}
	s.s = state.New(nil)
	s.ud = userdata.New(s.u, s.s)
	s.s.Lock()
}

func (s *userDataSuite) TearDownTest(c *C) {
	s.s.Unlock()
}

func (s *userDataSuite) TestGetUnsetKey(c *C) {
	var value string
	err := s.ud.Get("foo", &value)
	c.Assert(err, NotNil)
}

func (s *userDataSuite) TestGet(c *C) {
	s.s.Set("user-7-foo", "bar")

	var value string
	err := s.ud.Get("foo", &value)

	c.Assert(err, IsNil)
	c.Assert(value, Equals, "bar")
}

func (s *userDataSuite) TestSet(c *C) {
	s.ud.Set("foo", "bar")

	var value string
	err := s.s.Get("user-7-foo", &value)

	c.Assert(err, IsNil)
	c.Assert(value, Equals, "bar")
}
