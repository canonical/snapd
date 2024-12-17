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

package confdb_test

import (
	"github.com/snapcore/snapd/confdb"
	. "gopkg.in/check.v1"
)

type confdbCtrlSuite struct{}

var _ = Suite(&confdbCtrlSuite{})

func (s *confdbCtrlSuite) TestIsValidAuthenticationMethod(c *C) {
	c.Assert(confdb.IsValidAuthenticationMethod("operator-key"), Equals, true)
	c.Assert(confdb.IsValidAuthenticationMethod("store"), Equals, true)
	c.Assert(confdb.IsValidAuthenticationMethod("unknown"), Equals, false)
}

func (s *confdbCtrlSuite) TestConvertToAuthenticationMethods(c *C) {
	auth := []string{"operator-key", "store", "operator-key"}
	expected := []confdb.AuthenticationMethod{"operator-key", "store"} // duplicates removed
	converted, err := confdb.ConvertToAuthenticationMethods(auth)
	c.Assert(err, IsNil)
	c.Assert(converted, DeepEquals, expected)

	auth = []string{"operator-key", "unknown"}
	expected = nil
	converted, err = confdb.ConvertToAuthenticationMethods(auth)
	c.Assert(err, ErrorMatches, "invalid authentication method: unknown")
	c.Assert(converted, DeepEquals, expected)
}

func (s *confdbCtrlSuite) TestFindViewInGroup(c *C) {
	operator := confdb.Operator{ID: "jane"}

	err := operator.Delegate(
		[]string{
			"canonical/network/control-device",
			"canonical/network/control-interface",
			"canonical/network/observe-device",
			"canonical/network/observe-interface",
		},
		[]string{"operator-key"},
	)
	c.Assert(err, IsNil)

	type testcase struct {
		view  confdb.ViewRef
		idx   int
		found bool
	}
	tcs := []testcase{
		{
			view:  confdb.ViewRef{Account: "canonical", Confdb: "network", View: "observe-device"},
			idx:   2,
			found: true,
		},
		{
			view:  confdb.ViewRef{Account: "canonical", Confdb: "network", View: "control-device"},
			idx:   0,
			found: true,
		},
		{view: confdb.ViewRef{Account: "unknown", Confdb: "network", View: "control-device"}},
		{view: confdb.ViewRef{Account: "canonical", Confdb: "unknown", View: "control-device"}},
		{view: confdb.ViewRef{Account: "canonical", Confdb: "network", View: "unknown"}},
	}

	g := operator.Groups[0]
	for i, tc := range tcs {
		cmt := Commentf("test number %d", i+1)
		idx, found := g.FindView(&tc.view)
		c.Assert(found, Equals, tc.found, cmt)
		c.Assert(idx, Equals, tc.idx, cmt)
	}
}

func (s *confdbCtrlSuite) TestCompareViewRef(c *C) {
	observeDevice := confdb.ViewRef{Account: "canonical", Confdb: "device", View: "observe-device"}
	controlDevice := confdb.ViewRef{Account: "canonical", Confdb: "device", View: "control-device"}
	observeInterface := confdb.ViewRef{Account: "canonical", Confdb: "network", View: "observe-interface"}
	controlConfig := confdb.ViewRef{Account: "system", Confdb: "telemetry", View: "control-config"}

	type testcase struct {
		a      confdb.ViewRef
		b      confdb.ViewRef
		result int
	}
	tcs := []testcase{
		{a: observeDevice, b: observeDevice, result: 0},
		{a: observeDevice, b: controlDevice, result: 1},
		{a: controlDevice, b: observeDevice, result: -1},
		{a: observeInterface, b: controlDevice, result: 1},
		{a: controlDevice, b: observeInterface, result: -1},
		{a: controlConfig, b: controlDevice, result: 1},
		{a: controlDevice, b: controlConfig, result: -1},
	}
	for i, tc := range tcs {
		cmt := Commentf("test number %d", i+1)
		result := tc.a.Compare(&tc.b)
		c.Assert(result, Equals, tc.result, cmt)
	}
}

func (s *confdbCtrlSuite) TestGroupWithView(c *C) {
	operator := confdb.Operator{ID: "canonical"}

	err := operator.Delegate(
		[]string{"canonical/network/control-interface", "canonical/network/observe-interface"},
		[]string{"operator-key"},
	)
	c.Assert(err, IsNil)

	err = operator.Delegate(
		[]string{"canonical/network/control-device", "canonical/network/observe-device"},
		[]string{"store"},
	)
	c.Assert(err, IsNil)

	group, idx := operator.GroupWithView(&confdb.ViewRef{
		Account: "canonical", Confdb: "network", View: "control-interface",
	})
	c.Assert(group, Equals, operator.Groups[0])
	c.Assert(idx, Equals, 0)

	group, idx = operator.GroupWithView(&confdb.ViewRef{
		Account: "canonical", Confdb: "network", View: "observe-device",
	})
	c.Assert(group, Equals, operator.Groups[1])
	c.Assert(idx, Equals, 1)
}

func (s *confdbCtrlSuite) TestGroupWithAuthentication(c *C) {
	operator := confdb.Operator{ID: "canonical"}

	err := operator.Delegate(
		[]string{"canonical/network/control-interface"},
		[]string{"operator-key"},
	)
	c.Assert(err, IsNil)

	err = operator.Delegate(
		[]string{"canonical/network/observe-device"},
		[]string{"store"},
	)
	c.Assert(err, IsNil)

	err = operator.Delegate(
		[]string{"canonical/network/observe-interface"},
		[]string{"store", "operator-key"},
	)
	c.Assert(err, IsNil)

	group := operator.GroupWithAuthentication([]confdb.AuthenticationMethod{confdb.OperatorKey})
	c.Assert(group, Equals, operator.Groups[0])

	group = operator.GroupWithAuthentication([]confdb.AuthenticationMethod{confdb.Store})
	c.Assert(group, Equals, operator.Groups[1])

	group = operator.GroupWithAuthentication([]confdb.AuthenticationMethod{confdb.OperatorKey, confdb.Store})
	c.Assert(group, Equals, operator.Groups[2])
}

func (s *confdbCtrlSuite) TestDelegateOK(c *C) {
	operator := confdb.Operator{ID: "canonical"}

	views := []string{"canonical/network/control-device", "canonical/network/observe-device"}
	auth := []string{"operator-key", "store"}
	err := operator.Delegate(views, auth)
	c.Assert(err, IsNil)
	c.Assert(len(operator.Groups), Equals, 1)

	g := operator.Groups[0]
	expectedViews := []*confdb.ViewRef{
		{Account: "canonical", Confdb: "network", View: "control-device"},
		{Account: "canonical", Confdb: "network", View: "observe-device"},
	}
	c.Assert(g.Views, DeepEquals, expectedViews)
	expectedAuth := []confdb.AuthenticationMethod{confdb.OperatorKey, confdb.Store}
	c.Assert(g.Authentication, DeepEquals, expectedAuth)
}

func (s *confdbCtrlSuite) TestDelegateFail(c *C) {
	operator := confdb.Operator{ID: "canonical"}

	type testcase struct {
		views []string
		auth  []string
		err   string
	}
	tcs := []testcase{
		{err: `cannot delegate: "auth" must be a non-empty list`},
		{auth: []string{"magic"}, err: "cannot delegate: invalid authentication method: magic"},
		{auth: []string{"store"}, err: `cannot delegate: "views" must be a non-empty list`},
		{
			views: []string{"a/b/c/d"},
			auth:  []string{"store"},
			err:   `cannot delegate: view "a/b/c/d" must be in the format account/confdb/view`,
		},
		{
			views: []string{"a/b"},
			auth:  []string{"store"},
			err:   `cannot delegate: view "a/b" must be in the format account/confdb/view`,
		},
		{
			views: []string{"ab/"},
			auth:  []string{"store"},
			err:   `cannot delegate: view "ab/" must be in the format account/confdb/view`,
		},
		{
			views: []string{"@foo/network/control-device"},
			auth:  []string{"store"},
			err:   "cannot delegate: invalid Account ID @foo",
		},
		{
			views: []string{"canonical/123/control-device"},
			auth:  []string{"store"},
			err:   "cannot delegate: invalid confdb name 123",
		},
		{
			views: []string{"canonical/network/_view"},
			auth:  []string{"store"},
			err:   "cannot delegate: invalid view name _view",
		},
	}

	for i, tc := range tcs {
		cmt := Commentf("test number %d", i+1)
		err := operator.Delegate(tc.views, tc.auth)
		c.Assert(err, NotNil)
		c.Assert(err, ErrorMatches, tc.err, cmt)
	}
}

func (s *confdbCtrlSuite) TestRevoke(c *C) {
	operator := confdb.Operator{ID: "john"}

	err := operator.Delegate(
		[]string{"canonical/network/control-interface", "canonical/network/observe-interface"},
		[]string{"operator-key"},
	)
	c.Assert(err, IsNil)

	operator.Revoke(
		[]string{"canonical/network/control-interface"},
		[]string{"operator-key"},
	)
	delegated, err := operator.IsDelegated("canonical/network/control-interface", []string{"operator-key"})
	c.Check(err, IsNil)
	c.Check(delegated, Equals, false)

	// test idempotency
	operator.Revoke(
		[]string{"canonical/network/control-interface"},
		[]string{"operator-key"},
	)
	delegated, err = operator.IsDelegated("canonical/network/control-interface", []string{"operator-key"})
	c.Check(err, IsNil)
	c.Check(delegated, Equals, false)

	// revoke all auth
	err = operator.Delegate(
		[]string{"canonical/network/observe-interface", "canonical/network/control-vpn"},
		[]string{"store", "operator-key"},
	)
	c.Assert(err, IsNil)

	err = operator.Revoke([]string{"canonical/network/observe-interface"}, nil)
	c.Assert(err, IsNil)

	delegated, err = operator.IsDelegated("canonical/network/observe-interface", []string{"operator-key"})
	c.Check(err, IsNil)
	c.Check(delegated, Equals, false)

	delegated, err = operator.IsDelegated("canonical/network/observe-interface", []string{"store"})
	c.Check(err, IsNil)
	c.Check(delegated, Equals, false)
}

func (s *confdbCtrlSuite) TestRevokeFail(c *C) {
	operator := confdb.Operator{ID: "canonical"}

	type testcase struct {
		views []string
		auth  []string
		err   string
	}
	tcs := []testcase{
		{auth: []string{"magic"}, err: "cannot revoke: invalid authentication method: magic"},
		{
			views: []string{"invalid"},
			auth:  []string{"store"},
			err:   `cannot revoke: view "invalid" must be in the format account/confdb/view`,
		},
	}

	for i, tc := range tcs {
		cmt := Commentf("test number %d", i+1)
		err := operator.Revoke(tc.views, tc.auth)
		c.Assert(err, NotNil)
		c.Assert(err, ErrorMatches, tc.err, cmt)
	}
}
