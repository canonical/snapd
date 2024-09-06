// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package registry_test

import (
	"github.com/snapcore/snapd/registry"
	. "gopkg.in/check.v1"
)

type rgCtrlSuite struct{}

var _ = Suite(&rgCtrlSuite{})

func (s *rgCtrlSuite) TestAuthenticationMethods(c *C) {
	authMethod, _ := registry.StringToAuthenticationMethod("operator-key")
	c.Check(authMethod, Equals, registry.OperatorKey)
	c.Check(authMethod.String(), Equals, "operator-key")

	authMethod, err := registry.StringToAuthenticationMethod("foo-bar")
	c.Check(err, ErrorMatches, "unknown authentication method: foo-bar")
	c.Check(authMethod.String(), Equals, "unknown")
}

func (s *rgCtrlSuite) TestDelegateOK(c *C) {
	operator := registry.Operator{
		OperatorID: "jane",
		Groups:     make([]*registry.Group, 0),
	}

	err := operator.Delegate(
		[]string{"canonical/network/control-interface", "canonical/network/observe-interface"},
		[]registry.AuthenticationMethod{registry.Store},
	)
	c.Assert(err, IsNil)

	c.Check(operator.IsDelegated("canonical/network/control-vpn", registry.Store), Equals, false)

	err = operator.Delegate(
		[]string{"canonical/network/control-vpn"},
		[]registry.AuthenticationMethod{registry.Store},
	)
	c.Assert(err, IsNil)
	c.Check(operator.IsDelegated("canonical/network/control-vpn", registry.Store), Equals, true)
	c.Check(operator.IsDelegated("canonical/network/control-vpn", registry.OperatorKey), Equals, false)

	// test idempotency
	err = operator.Delegate(
		[]string{"canonical/network/control-vpn"},
		[]registry.AuthenticationMethod{registry.Store},
	)
	c.Assert(err, IsNil)
	c.Check(operator.IsDelegated("canonical/network/control-vpn", registry.Store), Equals, true)
}

func (s *rgCtrlSuite) TestDelegateFail(c *C) {
	operator := registry.Operator{
		OperatorID: "canonical",
		Groups:     make([]*registry.Group, 0),
	}

	type testcase struct {
		views          []string
		authentication []registry.AuthenticationMethod
		err            string
	}

	tcs := []testcase{
		{err: `"views" must be a non-empty list`},
		{
			views:          []string{"a/b/c/d"},
			authentication: []registry.AuthenticationMethod{registry.Store},
			err:            `"a/b/c/d" must be in the format account/registry/view`,
		},
		{
			views:          []string{"a/b"},
			authentication: []registry.AuthenticationMethod{registry.Store},
			err:            `"a/b" must be in the format account/registry/view`,
		},
		{
			views:          []string{"ab/"},
			authentication: []registry.AuthenticationMethod{registry.Store},
			err:            `"ab/" must be in the format account/registry/view`,
		},
		{
			views:          []string{"@foo/network/control-device"},
			authentication: []registry.AuthenticationMethod{registry.Store},
			err:            "invalid Account ID @foo",
		},
		{
			views:          []string{"canonical/123/control-device"},
			authentication: []registry.AuthenticationMethod{registry.Store},
			err:            "invalid registry name 123",
		},
		{
			views:          []string{"canonical/network/_view"},
			authentication: []registry.AuthenticationMethod{registry.Store},
			err:            "invalid view name _view",
		},
		{views: []string{"a/b/c"}, err: `"authentication" must be a non-empty list`},
	}

	for i, tc := range tcs {
		cmt := Commentf("test number %d", i+1)
		err := operator.Delegate(tc.views, tc.authentication)
		c.Assert(err, NotNil)
		c.Assert(err, ErrorMatches, tc.err, cmt)
	}
}

func (s *rgCtrlSuite) TestRevoke(c *C) {
	operator := registry.Operator{
		OperatorID: "john",
		Groups:     make([]*registry.Group, 0),
	}

	err := operator.Delegate(
		[]string{"canonical/network/control-interface", "canonical/network/observe-interface"},
		[]registry.AuthenticationMethod{registry.OperatorKey},
	)
	c.Assert(err, IsNil)

	operator.Revoke(
		[]string{"canonical/network/control-interface"},
		[]registry.AuthenticationMethod{registry.OperatorKey},
	)
	c.Check(operator.IsDelegated("canonical/network/control-interface", registry.OperatorKey), Equals, false)

	// test idempotency
	operator.Revoke(
		[]string{"canonical/network/control-interface"},
		[]registry.AuthenticationMethod{registry.OperatorKey},
	)
	c.Check(operator.IsDelegated("canonical/network/control-interface", registry.OperatorKey), Equals, false)

	// revoke all auth
	err = operator.Delegate(
		[]string{"canonical/network/observe-interface", "canonical/network/control-vpn"},
		[]registry.AuthenticationMethod{registry.Store, registry.OperatorKey},
	)
	c.Assert(err, IsNil)

	err = operator.Revoke([]string{"canonical/network/observe-interface"}, nil)
	c.Assert(err, IsNil)

	c.Check(operator.IsDelegated("canonical/network/observe-interface", registry.OperatorKey), Equals, false)
	c.Check(operator.IsDelegated("canonical/network/observe-interface", registry.Store), Equals, false)
}
