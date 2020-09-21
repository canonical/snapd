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

func (s *validationSetTrackingSuite) TestSet(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	all, err := assertstate.All(s.st)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 0)

	key := assertstate.ValidationTrackingKey{
		AccoundID: "foo",
		Name:      "bar",
	}
	tr := assertstate.ValidationSetTracking{
		Mode:      assertstate.Monitor,
		PinnedSeq: 1,
		LastSeq:   2,
	}
	assertstate.SetValidationTracking(s.st, key, &tr)

	all, err = assertstate.All(s.st)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 1)
	for k, v := range all {
		c.Check(k.AccoundID, Equals, "foo")
		c.Check(k.Name, Equals, "bar")
		c.Check(v, DeepEquals, &assertstate.ValidationSetTracking{Mode: assertstate.Monitor, PinnedSeq: 1, LastSeq: 2})
	}

	key = assertstate.ValidationTrackingKey{
		AccoundID: "foo",
		Name:      "baz",
	}
	tr = assertstate.ValidationSetTracking{
		Mode:    assertstate.Enforce,
		LastSeq: 3,
	}
	assertstate.SetValidationTracking(s.st, key, &tr)

	all, err = assertstate.All(s.st)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 2)

	var gotFirst, gotSecond bool
	for k, v := range all {
		if k.Name == "bar" {
			gotFirst = true
			c.Check(k.AccoundID, Equals, "foo")
			c.Check(k.Name, Equals, "bar")
			c.Check(v, DeepEquals, &assertstate.ValidationSetTracking{Mode: assertstate.Monitor, PinnedSeq: 1, LastSeq: 2})
		} else {
			gotSecond = true
			c.Check(k.AccoundID, Equals, "foo")
			c.Check(k.Name, Equals, "baz")
			c.Check(v, DeepEquals, &assertstate.ValidationSetTracking{Mode: assertstate.Enforce, PinnedSeq: 0, LastSeq: 3})
		}
	}
	c.Check(gotFirst, Equals, true)
	c.Check(gotSecond, Equals, true)
}

func (s *validationSetTrackingSuite) TestDelete(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	key := assertstate.ValidationTrackingKey{
		AccoundID: "foo",
		Name:      "bar",
	}

	// delete non-existing one is fine
	assertstate.SetValidationTracking(s.st, key, nil)
	all, err := assertstate.All(s.st)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 0)

	tr := assertstate.ValidationSetTracking{
		Mode: assertstate.Monitor,
	}
	assertstate.SetValidationTracking(s.st, key, &tr)

	all, err = assertstate.All(s.st)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 1)

	// deletes existing one
	assertstate.SetValidationTracking(s.st, key, nil)
	all, err = assertstate.All(s.st)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 0)
}

func (s *validationSetTrackingSuite) TestGet(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	key := assertstate.ValidationTrackingKey{
		AccoundID: "foo",
		Name:      "bar",
	}

	err := assertstate.GetValidationTracking(s.st, key, nil)
	c.Assert(err, ErrorMatches, `internal error: tr is nil`)

	tr := assertstate.ValidationSetTracking{
		Mode:    assertstate.Enforce,
		LastSeq: 3,
	}
	assertstate.SetValidationTracking(s.st, key, &tr)

	var res assertstate.ValidationSetTracking
	err = assertstate.GetValidationTracking(s.st, key, &res)
	c.Assert(err, IsNil)
	c.Check(res, DeepEquals, tr)

	// non-existing
	key = assertstate.ValidationTrackingKey{
		AccoundID: "foo",
		Name:      "baz",
	}
	err = assertstate.GetValidationTracking(s.st, key, &res)
	c.Assert(err, Equals, state.ErrNoState)
}
