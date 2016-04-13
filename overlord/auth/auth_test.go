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

package auth_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/overlord/auth"
	"github.com/ubuntu-core/snappy/overlord/state"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type authSuite struct {
	state *state.State
}

var _ = Suite(&authSuite{})

func (as *authSuite) SetUpTest(c *C) {
	as.state = state.New(nil)
}

func (as *authSuite) TestNewUser(c *C) {
	user, err := auth.NewUser(as.state, "username", "macaroon", []string{"discharge"})

	expected := &auth.AuthUser{
		ID:         1,
		Username:   "username",
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	}
	c.Check(err, IsNil)
	c.Check(user, DeepEquals, expected)

	userFromState, err := auth.User(as.state, 1)
	c.Check(err, IsNil)
	c.Check(userFromState, DeepEquals, expected)
}

func (as *authSuite) TestNewUserOverridesExistent(c *C) {
	_, err := auth.NewUser(as.state, "username", "macaroon", []string{"discharge"})
	c.Check(err, IsNil)

	// adding a new one
	user, err := auth.NewUser(as.state, "new_username", "new_macaroon", []string{"new_discharge"})
	expected := &auth.AuthUser{
		ID:         1,
		Username:   "new_username",
		Macaroon:   "new_macaroon",
		Discharges: []string{"new_discharge"},
	}
	c.Check(err, IsNil)
	c.Check(user, DeepEquals, expected)

	userFromState, err := auth.User(as.state, 1)
	c.Check(err, IsNil)
	c.Check(userFromState, DeepEquals, expected)
}

func (as *authSuite) TestUserForNoAuthInState(c *C) {
	userFromState, err := auth.User(as.state, 42)
	c.Check(err, NotNil)
	c.Check(userFromState, IsNil)
}

func (as *authSuite) TestUserForNonExistent(c *C) {
	_, err := auth.NewUser(as.state, "username", "macaroon", []string{"discharge"})
	c.Check(err, IsNil)

	userFromState, err := auth.User(as.state, 42)
	c.Check(err, ErrorMatches, "invalid user")
	c.Check(userFromState, IsNil)
}

func (as *authSuite) TestUser(c *C) {
	user, err := auth.NewUser(as.state, "username", "macaroon", []string{"discharge"})
	c.Check(err, IsNil)

	userFromState, err := auth.User(as.state, 1)
	c.Check(err, IsNil)
	c.Check(userFromState, DeepEquals, user)
}
