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
	"github.com/snapcore/snapd/testutil"
	. "gopkg.in/check.v1"
)

type ctrlSuite struct{}

var _ = Suite(&ctrlSuite{})

func (s *ctrlSuite) TestNewAuthentication(c *C) {
	authMeth := []string{"operator-key", "store", "operator-key"}
	expected := confdb.OperatorKey | confdb.Store
	converted, err := confdb.NewAuthentication(authMeth)
	c.Assert(err, IsNil)
	c.Assert(converted, DeepEquals, expected)

	authMeth = []string{"operator-key", "unknown"}
	expected = 0
	converted, err = confdb.NewAuthentication(authMeth)
	c.Assert(err, ErrorMatches, "invalid authentication method: unknown")
	c.Assert(converted, DeepEquals, expected)
}

func (s *ctrlSuite) TestConvertAuthenticationToStrings(c *C) {
	var auth confdb.Authentication = 0
	var expected []string
	c.Assert(auth.ToStrings(), DeepEquals, expected)

	auth |= confdb.OperatorKey
	expected = append(expected, "operator-key")
	c.Assert(auth.ToStrings(), DeepEquals, expected)

	auth |= confdb.Store
	expected = append(expected, "store")
	c.Assert(auth.ToStrings(), DeepEquals, expected)
}

func (s *ctrlSuite) TestViewRefString(c *C) {
	view := confdb.ViewRef{Account: "canonical", Confdb: "network", View: "control-device"}
	c.Assert(view.String(), Equals, "canonical/network/control-device")
}

func (s *ctrlSuite) TestDelegateOK(c *C) {
	cc := confdb.Control{}
	cc.Delegate(
		"alice",
		[]string{"canonical/device/control-device", "canonical/device/observe-device"},
		[]string{"operator-key"},
	)
	cc.Delegate(
		"alice",
		[]string{"canonical/device/control-device", "canonical/network/observe-interface"},
		[]string{"operator-key", "store"},
	)

	delegated, _ := cc.IsDelegated("alice", "canonical/device/observe-device", []string{"operator-key"})
	c.Check(delegated, Equals, true)

	delegated, _ = cc.IsDelegated("alice", "canonical/device/control-device", []string{"operator-key", "store"})
	c.Check(delegated, Equals, true)

	delegated, _ = cc.IsDelegated("alice", "canonical/network/observe-interface", []string{"store", "operator-key"})
	c.Check(delegated, Equals, true)
}

func (s *ctrlSuite) TestDelegateFail(c *C) {
	cc := confdb.Control{}

	type testcase struct {
		operator string
		views    []string
		auth     []string
		err      string
	}
	tcs := []testcase{
		{err: "invalid operator ID: "},
		{
			operator: "alice",
			err:      `cannot delegate: "authentications" must be a non-empty list`,
		},
		{
			operator: "alice",
			auth:     []string{"magic"},
			err:      "cannot delegate: invalid authentication method: magic",
		},
		{
			operator: "alice",
			auth:     []string{"store"},
			err:      `cannot delegate: "views" must be a non-empty list`,
		},
		{
			operator: "alice",
			views:    []string{"a/b/c/d"},
			auth:     []string{"store"},
			err:      `cannot delegate: view "a/b/c/d" must be in the format account/confdb/view`,
		},
		{
			operator: "alice",
			views:    []string{"a/b"},
			auth:     []string{"store"},
			err:      `cannot delegate: view "a/b" must be in the format account/confdb/view`,
		},
		{
			operator: "alice",
			views:    []string{"ab/"},
			auth:     []string{"store"},
			err:      `cannot delegate: view "ab/" must be in the format account/confdb/view`,
		},
		{
			operator: "alice",
			views:    []string{"@foo/network/control-device"},
			auth:     []string{"store"},
			err:      "cannot delegate: invalid account ID: @foo",
		},
		{
			operator: "alice",
			views:    []string{"canonical/123/control-device"},
			auth:     []string{"store"},
			err:      "cannot delegate: invalid confdb name: 123",
		},
		{
			operator: "alice",
			views:    []string{"canonical/network/_view"},
			auth:     []string{"store"},
			err:      "cannot delegate: invalid view name: _view",
		},
	}

	for i, tc := range tcs {
		cmt := Commentf("test number %d", i+1)
		err := cc.Delegate(tc.operator, tc.views, tc.auth)
		c.Assert(err, NotNil, cmt)
		c.Assert(err, ErrorMatches, tc.err, cmt)
	}
}

func (s *ctrlSuite) TestUndelegateOK(c *C) {
	cc := confdb.Control{}
	err := cc.Delegate(
		"bob",
		[]string{"canonical/network/control-interface", "canonical/network/observe-interface"},
		[]string{"operator-key", "store"},
	)
	c.Assert(err, IsNil)

	err = cc.Undelegate(
		"bob",
		[]string{"canonical/network/control-interface"},
		[]string{"operator-key"},
	)
	c.Assert(err, IsNil)
	delegated, err := cc.IsDelegated("bob", "canonical/network/control-interface", []string{"operator-key"})
	c.Assert(err, IsNil)
	c.Check(delegated, Equals, false)

	// undelegate non-existing view
	err = cc.Undelegate("bob", []string{"canonical/network/unknown"}, []string{"operator-key"})
	c.Assert(err, IsNil)

	// undelegate everything
	err = cc.Undelegate("bob", nil, nil)
	c.Assert(err, IsNil)

	delegated, err = cc.IsDelegated("bob", "canonical/network/observe-interface", []string{"operator-key"})
	c.Assert(err, IsNil)
	c.Check(delegated, Equals, false)

	delegated, err = cc.IsDelegated("bob", "canonical/network/observe-interface", []string{"store"})
	c.Assert(err, IsNil)
	c.Check(delegated, Equals, false)

	// undelegate non-existing operator
	err = cc.Undelegate("unknown", nil, nil)
	c.Assert(err, IsNil)
}

func (s *ctrlSuite) TestUndelegateFail(c *C) {
	cc := confdb.Control{}
	cc.Delegate("alice", []string{"aa/bb/cc"}, []string{"store"})

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
		err := cc.Undelegate("alice", tc.views, tc.auth)
		c.Assert(err, NotNil, cmt)
		c.Assert(err, ErrorMatches, tc.err, cmt)
	}
}

func (s *ctrlSuite) TestIsDelegatedOK(c *C) {
	cc := confdb.Control{}
	cc.Delegate(
		"alice",
		[]string{"canonical/device/control-device", "canonical/device/observe-device"},
		[]string{"operator-key"},
	)
	cc.Delegate(
		"alice",
		[]string{"canonical/device/control-device", "canonical/network/observe-interface"},
		[]string{"operator-key", "store"},
	)

	delegated, _ := cc.IsDelegated("alice", "canonical/device/control-device", []string{"store"})
	c.Check(delegated, Equals, true)
	delegated, _ = cc.IsDelegated("alice", "canonical/device/control-device", []string{"store", "operator-key"})
	c.Check(delegated, Equals, true)

	delegated, err := cc.IsDelegated("alice", "canonical/device/observe-device", []string{"store"})
	c.Check(err, IsNil)
	c.Check(delegated, Equals, false)

	delegated, _ = cc.IsDelegated("alice", "canonical/unknown/unknown", []string{"operator-key"})
	c.Check(err, IsNil)
	c.Check(delegated, Equals, false)

	delegated, _ = cc.IsDelegated("unknown", "canonical/unknown/unknown", []string{"operator-key"})
	c.Check(err, IsNil)
	c.Check(delegated, Equals, false)
}

func (s *ctrlSuite) TestIsDelegatedFail(c *C) {
	cc := confdb.Control{}
	cc.Delegate("bob", []string{"aa/bb/cc"}, []string{"store"})

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
		delegated, err := cc.IsDelegated("bob", tc.view, tc.auth)
		c.Assert(err, NotNil, cmt)
		c.Assert(err, ErrorMatches, tc.err, cmt)
		c.Assert(delegated, Equals, false, cmt)
	}
}

func (s *ctrlSuite) TestGroups(c *C) {
	cc := confdb.Control{}

	cc.Delegate("aa", []string{"dd/ee/ff", "gg/hh/ii", "jj/kk/ll"}, []string{"store", "operator-key"})
	cc.Delegate("aa", []string{"pp/qq/rr"}, []string{"operator-key"})
	cc.Delegate("aa", []string{"mm/nn/oo"}, []string{"store"})
	cc.Delegate("aa", []string{"ss/tt/vv"}, []string{"store", "operator-key"})

	cc.Delegate("bb", []string{"dd/ee/ff", "gg/hh/ii", "jj/kk/ll", "xx/yy/zz"}, []string{"operator-key", "store"})
	cc.Delegate("bb", []string{"mm/nn/oo"}, []string{"store"})
	cc.Delegate("bb", []string{"aa/bb/cc"}, []string{"operator-key"})

	cc.Delegate("cc", []string{"dd/ee/ff", "gg/hh/ii", "jj/kk/ll", "xx/yy/zz"}, []string{"store", "operator-key"})
	cc.Delegate("cc", []string{"pp/qq/rr"}, []string{"operator-key"})

	groups := cc.Groups()
	c.Assert(groups, HasLen, 6)
	expectedGroups := []any{
		map[string]any{
			"operators":       []any{"aa", "cc"},
			"authentications": []any{"operator-key"},
			"views":           []any{"pp/qq/rr"},
		},
		map[string]any{
			"operators":       []any{"bb"},
			"authentications": []any{"operator-key"},
			"views":           []any{"aa/bb/cc"},
		},
		map[string]any{
			"operators":       []any{"aa", "bb"},
			"authentications": []any{"store"},
			"views":           []any{"mm/nn/oo"},
		},
		map[string]any{
			"operators":       []any{"bb", "cc"},
			"authentications": []any{"operator-key", "store"},
			"views":           []any{"xx/yy/zz"},
		},
		map[string]any{
			"operators":       []any{"aa", "bb", "cc"},
			"authentications": []any{"operator-key", "store"},
			"views":           []any{"dd/ee/ff", "gg/hh/ii", "jj/kk/ll"},
		},
		map[string]any{
			"operators":       []any{"aa"},
			"authentications": []any{"operator-key", "store"},
			"views":           []any{"ss/tt/vv"},
		},
	}
	for _, expected := range expectedGroups {
		c.Assert(groups, testutil.DeepContains, expected)
	}
}

func (s *ctrlSuite) TestClone(c *C) {
	original := confdb.Control{}
	original.Delegate("aa", []string{"dd/ee/ff", "gg/hh/ii"}, []string{"store", "operator-key"})

	clone := original.Clone()
	clone.Undelegate("aa", []string{"dd/ee/ff"}, []string{"store"})

	// confirm that modifying the clone does not affect the original
	delegated, err := original.IsDelegated("aa", "dd/ee/ff", []string{"store"})
	c.Assert(err, IsNil)
	c.Assert(delegated, Equals, true)

	delegated, err = clone.IsDelegated("aa", "dd/ee/ff", []string{"store"})
	c.Assert(err, IsNil)
	c.Assert(delegated, Equals, false)
}
