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

func (s *confdbCtrlSuite) TestConvertStringsToAuthentication(c *C) {
	rawAuth := []string{"operator-key", "store", "operator-key"}
	expected := confdb.OperatorKey | confdb.Store
	converted, err := confdb.ConvertStringsToAuthentication(rawAuth)
	c.Assert(err, IsNil)
	c.Assert(converted, DeepEquals, expected)

	rawAuth = []string{"operator-key", "unknown"}
	expected = 0
	converted, err = confdb.ConvertStringsToAuthentication(rawAuth)
	c.Assert(err, ErrorMatches, "invalid authentication method: unknown")
	c.Assert(converted, DeepEquals, expected)
}

func (s *confdbCtrlSuite) TestConvertAuthenticationToStrings(c *C) {
	var auth confdb.Authentication = 0
	expected := []string{}
	c.Assert(confdb.ConvertAuthenticationToStrings(auth), DeepEquals, expected)

	auth |= confdb.OperatorKey
	expected = append(expected, "operator-key")
	c.Assert(confdb.ConvertAuthenticationToStrings(auth), DeepEquals, expected)

	auth |= confdb.Store
	expected = append(expected, "store")
	c.Assert(confdb.ConvertAuthenticationToStrings(auth), DeepEquals, expected)
}

func (s *confdbCtrlSuite) TestViewRefString(c *C) {
	view := confdb.ViewRef{Account: "canonical", Confdb: "network", View: "control-device"}
	c.Assert(view.String(), Equals, "canonical/network/control-device")
}

func (s *confdbCtrlSuite) TestDelegateOK(c *C) {
	op := confdb.Operator{ID: "canonical"}
	op.Delegate(
		[]string{"canonical/device/control-device", "canonical/device/observe-device"},
		[]string{"operator-key"},
	)
	op.Delegate(
		[]string{"canonical/device/control-device", "canonical/network/observe-interface"},
		[]string{"operator-key", "store"},
	)

	observeDevice := confdb.ViewRef{Account: "canonical", Confdb: "device", View: "observe-device"}
	controlDevice := confdb.ViewRef{Account: "canonical", Confdb: "device", View: "control-device"}
	observeInterface := confdb.ViewRef{Account: "canonical", Confdb: "network", View: "observe-interface"}

	c.Assert(op.Delegations[observeDevice], DeepEquals, confdb.OperatorKey)
	c.Assert(op.Delegations[controlDevice], DeepEquals, confdb.OperatorKey|confdb.Store)
	c.Assert(op.Delegations[observeInterface], DeepEquals, confdb.OperatorKey|confdb.Store)
}

func (s *confdbCtrlSuite) TestDelegateFail(c *C) {
	op := confdb.Operator{ID: "canonical"}

	type testcase struct {
		views []string
		auth  []string
		err   string
	}
	tcs := []testcase{
		{err: `cannot delegate: "authentications" must be a non-empty list`},
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

func (s *confdbCtrlSuite) TestUndelegateOK(c *C) {
	op := confdb.Operator{ID: "john"}
	err := op.Delegate(
		[]string{"canonical/network/control-interface", "canonical/network/observe-interface"},
		[]string{"operator-key", "store"},
	)
	c.Assert(err, IsNil)

	err = op.Undelegate([]string{"canonical/network/control-interface"}, []string{"operator-key"})
	c.Assert(err, IsNil)
	delegated, err := op.IsDelegated("canonical/network/control-interface", []string{"operator-key"})
	c.Assert(err, IsNil)
	c.Check(delegated, Equals, false)

	// undelegate non-existing view
	err = op.Undelegate([]string{"canonical/network/unknown"}, []string{"operator-key"})
	c.Assert(err, IsNil)

	// undelegate everything
	err = op.Undelegate(nil, nil)
	c.Assert(err, IsNil)
	c.Assert(op.Delegations, HasLen, 0)

	delegated, err = op.IsDelegated("canonical/network/observe-interface", []string{"operator-key"})
	c.Assert(err, IsNil)
	c.Check(delegated, Equals, false)

	delegated, err = op.IsDelegated("canonical/network/observe-interface", []string{"store"})
	c.Assert(err, IsNil)
	c.Check(delegated, Equals, false)
}

func (s *confdbCtrlSuite) TestUndelegateFail(c *C) {
	op := confdb.Operator{ID: "canonical"}

	type testcase struct {
		views []string
		auth  []string
		err   string
	}
	tcs := []testcase{
		{
			views: []string{"canonical/network/observe-interface"},
			auth:  []string{"magic"},
			err:   "cannot undelegate: invalid authentication method: magic",
		},
		{
			views: []string{"invalid"},
			auth:  []string{"store"},
			err:   `cannot undelegate: view "invalid" must be in the format account/confdb/view`,
		},
	}

	for i, tc := range tcs {
		cmt := Commentf("test number %d", i+1)
		err := op.Undelegate(tc.views, tc.auth)
		c.Assert(err, NotNil, cmt)
		c.Assert(err, ErrorMatches, tc.err, cmt)
	}
}

func (s *confdbCtrlSuite) TestIsDelegatedOK(c *C) {
	op := confdb.Operator{ID: "canonical"}
	op.Delegate(
		[]string{"canonical/device/control-device", "canonical/device/observe-device"},
		[]string{"operator-key"},
	)
	op.Delegate(
		[]string{"canonical/device/control-device", "canonical/network/observe-interface"},
		[]string{"operator-key", "store"},
	)

	delegated, _ := op.IsDelegated("canonical/device/control-device", []string{"store"})
	c.Check(delegated, Equals, true)
	delegated, _ = op.IsDelegated("canonical/device/control-device", []string{"store", "operator-key"})
	c.Check(delegated, Equals, true)

	delegated, err := op.IsDelegated("canonical/device/observe-device", []string{"store"})
	c.Check(err, IsNil)
	c.Check(delegated, Equals, false)

	delegated, _ = op.IsDelegated("canonical/unknown/unknown", []string{"operator-key"})
	c.Check(err, IsNil)
	c.Check(delegated, Equals, false)
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
