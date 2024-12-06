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

func (s *confdbCtrlSuite) TestAddGroupOK(c *C) {
	operator := confdb.Operator{ID: "canonical"}

	views := []string{"canonical/network/control-device", "canonical/network/observe-device"}
	auth := []string{"operator-key", "store"}
	err := operator.AddControlGroup(views, auth)
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

func (s *confdbCtrlSuite) TestAddGroupFail(c *C) {
	operator := confdb.Operator{ID: "canonical"}

	type testcase struct {
		views []string
		auth  []string
		err   string
	}
	tcs := []testcase{
		{err: `cannot add group: "auth" must be a non-empty list`},
		{auth: []string{"magic"}, err: "cannot add group: invalid authentication method: magic"},
		{auth: []string{"store"}, err: `cannot add group: "views" must be a non-empty list`},
		{
			views: []string{"a/b/c/d"},
			auth:  []string{"store"},
			err:   `view "a/b/c/d" must be in the format account/confdb/view`,
		},
		{
			views: []string{"a/b"},
			auth:  []string{"store"},
			err:   `view "a/b" must be in the format account/confdb/view`,
		},
		{
			views: []string{"ab/"},
			auth:  []string{"store"},
			err:   `view "ab/" must be in the format account/confdb/view`,
		},
		{
			views: []string{"@foo/network/control-device"},
			auth:  []string{"store"},
			err:   "invalid Account ID @foo",
		},
		{
			views: []string{"canonical/123/control-device"},
			auth:  []string{"store"},
			err:   "invalid confdb name 123",
		},
		{
			views: []string{"canonical/network/_view"},
			auth:  []string{"store"},
			err:   "invalid view name _view",
		},
	}

	for i, tc := range tcs {
		cmt := Commentf("test number %d", i+1)
		err := operator.AddControlGroup(tc.views, tc.auth)
		c.Assert(err, NotNil)
		c.Assert(err, ErrorMatches, tc.err, cmt)
	}
}
