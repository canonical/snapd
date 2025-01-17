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
		result := tc.a.Compare(&tc.b)
		c.Assert(result, Equals, tc.result, Commentf("test number %d", i+1))
	}
}

func (s *confdbCtrlSuite) TestGroupWithView(c *C) {
	op := confdb.Operator{ID: "canonical"}
	op.Delegate(
		[]string{"canonical/network/control-interface", "canonical/network/observe-interface"},
		[]string{"operator-key"},
	)
	op.Delegate(
		[]string{"canonical/network/control-device", "canonical/network/observe-device"},
		[]string{"store"},
	)

	group, idx := op.GroupWithView(&confdb.ViewRef{
		Account: "canonical", Confdb: "network", View: "control-interface",
	})
	c.Assert(group, Equals, op.Groups[0])
	c.Assert(idx, Equals, 0)

	group, idx = op.GroupWithView(&confdb.ViewRef{
		Account: "canonical", Confdb: "network", View: "observe-device",
	})
	c.Assert(group, Equals, op.Groups[1])
	c.Assert(idx, Equals, 1)
}

func (s *confdbCtrlSuite) TestGroupWithAuthentication(c *C) {
	op := confdb.Operator{ID: "canonical"}
	op.Delegate([]string{"canonical/network/control-interface"}, []string{"operator-key"})
	op.Delegate([]string{"canonical/network/observe-device"}, []string{"store"})
	op.Delegate([]string{"canonical/network/observe-interface"}, []string{"store", "operator-key"})

	group := op.GroupWithAuthentication([]confdb.AuthenticationMethod{confdb.OperatorKey})
	c.Assert(group, Equals, op.Groups[0])

	group = op.GroupWithAuthentication([]confdb.AuthenticationMethod{confdb.Store})
	c.Assert(group, Equals, op.Groups[1])

	group = op.GroupWithAuthentication([]confdb.AuthenticationMethod{confdb.OperatorKey, confdb.Store})
	c.Assert(group, Equals, op.Groups[2])
}

func (s *confdbCtrlSuite) TestDelegateOK(c *C) {
	op := confdb.Operator{ID: "canonical"}
	op.Delegate(
		[]string{"canonical/network/control-device", "canonical/network/observe-device"},
		[]string{"operator-key"},
	)
	op.Delegate(
		[]string{"canonical/network/control-device", "canonical/network/observe-device"},
		[]string{"operator-key", "store"},
	)

	expectedViews := []*confdb.ViewRef{
		{Account: "canonical", Confdb: "network", View: "control-device"},
		{Account: "canonical", Confdb: "network", View: "observe-device"},
	}
	c.Assert(op.Groups[0].Views, DeepEquals, expectedViews)
	expectedAuth := []confdb.AuthenticationMethod{confdb.OperatorKey, confdb.Store}
	c.Assert(op.Groups[0].Authentication, DeepEquals, expectedAuth)

	// test idempotency
	err := op.Delegate([]string{"canonical/network/control-device"}, []string{"operator-key", "store"})
	c.Assert(err, IsNil)
	c.Assert(len(op.Groups), Equals, 1)

	delegated, _ := op.IsDelegated("canonical/network/control-device", []string{"store"})
	c.Check(delegated, Equals, true)
}

func (s *confdbCtrlSuite) TestDelegateFail(c *C) {
	op := confdb.Operator{ID: "canonical"}

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
		err := op.Delegate(tc.views, tc.auth)
		c.Assert(err, NotNil, cmt)
		c.Assert(err, ErrorMatches, tc.err, cmt)
	}
}

func (s *confdbCtrlSuite) TestRevokeOK(c *C) {
	op := confdb.Operator{ID: "john"}
	err := op.Delegate(
		[]string{"canonical/network/control-interface", "canonical/network/observe-interface"},
		[]string{"operator-key", "store"},
	)
	c.Assert(err, IsNil)

	op.Revoke([]string{"canonical/network/control-interface"}, []string{"operator-key"})
	delegated, err := op.IsDelegated("canonical/network/control-interface", []string{"operator-key"})
	c.Check(err, IsNil)
	c.Check(delegated, Equals, false)

	// revoke non-existing view
	err = op.Revoke([]string{"canonical/network/unknown"}, []string{"operator-key"})
	c.Check(err, IsNil)

	// revoke all auth
	err = op.Delegate(
		[]string{"canonical/network/observe-interface", "canonical/network/control-vpn"},
		[]string{"store", "operator-key"},
	)
	c.Assert(err, IsNil)

	err = op.Revoke(nil, nil)
	c.Assert(err, IsNil)

	delegated, err = op.IsDelegated("canonical/network/observe-interface", []string{"operator-key"})
	c.Check(err, IsNil)
	c.Check(delegated, Equals, false)

	delegated, err = op.IsDelegated("canonical/network/observe-interface", []string{"store"})
	c.Check(err, IsNil)
	c.Check(delegated, Equals, false)
}

func (s *confdbCtrlSuite) TestRevokeFail(c *C) {
	op := confdb.Operator{ID: "canonical"}

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
		err := op.Revoke(tc.views, tc.auth)
		c.Assert(err, NotNil, cmt)
		c.Assert(err, ErrorMatches, tc.err, cmt)
	}
}

func (s *confdbCtrlSuite) TestIsDelegatedFail(c *C) {
	op := confdb.Operator{ID: "canonical"}

	type testcase struct {
		view string
		auth []string
		err  string
	}
	tcs := []testcase{
		{
			view: "invalid",
			auth: []string{"store"},
			err:  `view "invalid" must be in the format account/confdb/view`,
		},
		{
			view: "canonical/network/control-device",
			auth: []string{"magic"},
			err:  "invalid authentication method: magic",
		},
	}

	for i, tc := range tcs {
		cmt := Commentf("test number %d", i+1)
		delegated, err := op.IsDelegated(tc.view, tc.auth)
		c.Assert(err, NotNil, cmt)
		c.Assert(err, ErrorMatches, tc.err, cmt)
		c.Assert(delegated, Equals, false, cmt)
	}
}

func (s *confdbCtrlSuite) TestViewRefString(c *C) {
	view := confdb.ViewRef{Account: "canonical", Confdb: "network", View: "control-device"}
	c.Assert(view.String(), Equals, "canonical/network/control-device")
}
