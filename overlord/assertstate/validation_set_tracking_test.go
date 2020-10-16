// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package assertstate_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/state"
)

type validationSetTrackingSuite struct {
	st *state.State
}

var _ = Suite(&validationSetTrackingSuite{})

func (s *validationSetTrackingSuite) SetUpTest(c *C) {
	s.st = state.New(nil)
}

func (s *validationSetTrackingSuite) TestUpdate(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	all, err := assertstate.ValidationSets(s.st)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 0)

	tr := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Enforce,
		PinnedAt:  1,
		Current:   2,
	}
	assertstate.UpdateValidationSet(s.st, &tr)

	all, err = assertstate.ValidationSets(s.st)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 1)
	for k, v := range all {
		c.Check(k, Equals, "foo/bar")
		c.Check(v, DeepEquals, &assertstate.ValidationSetTracking{AccountID: "foo", Name: "bar", Mode: assertstate.Enforce, PinnedAt: 1, Current: 2})
	}

	tr = assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Monitor,
		PinnedAt:  2,
		Current:   3,
	}
	assertstate.UpdateValidationSet(s.st, &tr)

	all, err = assertstate.ValidationSets(s.st)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 1)
	for k, v := range all {
		c.Check(k, Equals, "foo/bar")
		c.Check(v, DeepEquals, &assertstate.ValidationSetTracking{AccountID: "foo", Name: "bar", Mode: assertstate.Monitor, PinnedAt: 2, Current: 3})
	}

	tr = assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "baz",
		Mode:      assertstate.Enforce,
		Current:   3,
	}
	assertstate.UpdateValidationSet(s.st, &tr)

	all, err = assertstate.ValidationSets(s.st)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 2)

	var gotFirst, gotSecond bool
	for k, v := range all {
		if k == "foo/bar" {
			gotFirst = true
			c.Check(v, DeepEquals, &assertstate.ValidationSetTracking{AccountID: "foo", Name: "bar", Mode: assertstate.Monitor, PinnedAt: 2, Current: 3})
		} else {
			gotSecond = true
			c.Check(k, Equals, "foo/baz")
			c.Check(v, DeepEquals, &assertstate.ValidationSetTracking{AccountID: "foo", Name: "baz", Mode: assertstate.Enforce, PinnedAt: 0, Current: 3})
		}
	}
	c.Check(gotFirst, Equals, true)
	c.Check(gotSecond, Equals, true)
}

func (s *validationSetTrackingSuite) TestDelete(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	// delete non-existing one is fine
	assertstate.DeleteValidationSet(s.st, "foo", "bar")
	all, err := assertstate.ValidationSets(s.st)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 0)

	tr := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Monitor,
	}
	assertstate.UpdateValidationSet(s.st, &tr)

	all, err = assertstate.ValidationSets(s.st)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 1)

	// deletes existing one
	assertstate.DeleteValidationSet(s.st, "foo", "bar")
	all, err = assertstate.ValidationSets(s.st)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 0)
}

func (s *validationSetTrackingSuite) TestGet(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	err := assertstate.GetValidationSet(s.st, "foo", "bar", nil)
	c.Assert(err, ErrorMatches, `internal error: tr is nil`)

	tr := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Enforce,
		Current:   3,
	}
	assertstate.UpdateValidationSet(s.st, &tr)

	var res assertstate.ValidationSetTracking
	err = assertstate.GetValidationSet(s.st, "foo", "bar", &res)
	c.Assert(err, IsNil)
	c.Check(res, DeepEquals, tr)

	// non-existing
	err = assertstate.GetValidationSet(s.st, "foo", "baz", &res)
	c.Assert(err, Equals, state.ErrNoState)
}
