// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package snap_test

import (
	"errors"
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap"
)

type errorsSuite struct{}

var _ = Suite(&errorsSuite{})

func (s *errorsSuite) TestAlreadyInstalledError(c *C) {

	for _, testCase := range []struct {
		snaps       []string
		components  map[string][]string
		expectedStr string
	}{
		{
			[]string{"some-snap"},
			nil,
			`snap "some-snap" is already installed`,
		},
		{
			nil,
			map[string][]string{"some-snap": {"comp"}},
			`component "some-snap\+comp" is already installed`,
		},
		// check that snap names are sorted
		{
			[]string{"some-snap", "other-snap"},
			nil,
			`snaps "other-snap,some-snap" are already installed`,
		},
		{
			nil,
			map[string][]string{"some-snap": {"comp1", "comp2"}},
			`components "some-snap\+comp1,some-snap\+comp2" are already installed`,
		},
		{
			nil,
			map[string][]string{"some-snap": {"comp1"}, "other-snap": {"comp2"}},
			`components "other-snap\+comp2,some-snap\+comp1" are already installed`,
		},
		{
			[]string{"some-snap", "other-snap"},
			map[string][]string{"some-snap": {"comp"}},
			`snaps "other-snap,some-snap" and component "some-snap\+comp" are already installed`,
		},
		{
			[]string{"some-snap"},
			map[string][]string{"other-snap": {"comp1", "comp2"}},
			`snap "some-snap" and components "other-snap\+comp1,other-snap\+comp2" are already installed`,
		},
		// check that component names are sorted
		{
			[]string{"some-snap"},
			map[string][]string{"other-snap": {"comp"}, "some-other-snap": {"comp"}},
			`snap "some-snap" and components "other-snap\+comp,some-other-snap\+comp" are already installed`,
		},
		{
			[]string{"some-snap", "other-snap"},
			map[string][]string{"other-snap": {"comp"}, "some-other-snap": {"comp"}},
			`snaps "other-snap,some-snap" and components "other-snap\+comp,some-other-snap\+comp" are already installed`,
		},
	} {
		err := snap.NewAlreadyInstalledError(testCase.snaps, testCase.components)
		c.Check(err, ErrorMatches, testCase.expectedStr)
	}

	err := snap.AlreadyInstalledError{
		Snaps:      []string{"foo", "bar"},
		Components: map[string][]string{"some-snap": {"comp1", "comp2"}},
	}
	c.Check(errors.Is(err, err), Equals, true)

	// Different error type should not match
	c.Check(errors.Is(err, errors.New("some other error")), Equals, false)
	// nil - should not match
	c.Check(errors.Is(err, nil), Equals, false)

	// different snap order should not match
	otherErr := snap.AlreadyInstalledError{
		Snaps:      []string{"bar", "foo"},
		Components: map[string][]string{"some-snap": {"comp1", "comp2"}},
	}
	c.Check(errors.Is(err, otherErr), Equals, false)

	// different component order should not match
	otherErr = snap.AlreadyInstalledError{
		Snaps:      []string{"foo", "bar"},
		Components: map[string][]string{"some-snap": {"comp2", "comp1"}},
	}
	c.Check(errors.Is(err, otherErr), Equals, false)

	// different snaps should not match
	otherErr = snap.AlreadyInstalledError{
		Snaps:      []string{"foo"},
		Components: map[string][]string{"some-snap": {"comp1", "comp2"}},
	}
	c.Check(errors.Is(err, otherErr), Equals, false)

	// different snap for component should not match
	otherErr = snap.AlreadyInstalledError{
		Snaps:      []string{"foo", "bar"},
		Components: map[string][]string{"other-snap": {"comp1", "comp2"}},
	}
	c.Check(errors.Is(err, otherErr), Equals, false)

	// different number of components should not match
	otherErr = snap.AlreadyInstalledError{
		Snaps:      []string{"foo", "bar"},
		Components: map[string][]string{"some-snap": {"comp1", "comp2"}, "other-snap": {"comp"}},
	}
	c.Check(errors.Is(err, otherErr), Equals, false)

	// different components should not match
	otherErr = snap.AlreadyInstalledError{
		Snaps:      []string{"bar", "foo"},
		Components: map[string][]string{"some-snap": {"comp1"}},
	}
	c.Check(errors.Is(err, otherErr), Equals, false)

	// check that the error generated with snap.NewAlreadyInstalledError matches
	otherErr2 := snap.NewAlreadyInstalledError([]string{"foo", "bar"}, map[string][]string{"some-snap": {"comp1"}})
	c.Check(errors.Is(otherErr, *otherErr2), Equals, true)

	otherErr = snap.AlreadyInstalledError{
		Snaps: []string{"foo", "bar"},
	}
	c.Check(errors.Is(err, otherErr), Equals, false)

	// check that snap.NewAlreadyInstalledSnapsError sorts the snaps in the resulting error
	c.Check(errors.Is(otherErr, snap.NewAlreadyInstalledSnapsError([]string{"foo", "bar"})), Equals, false)
	otherErr = snap.AlreadyInstalledError{
		Snaps: []string{"bar", "foo"},
	}
	c.Check(errors.Is(otherErr, *snap.NewAlreadyInstalledSnapsError([]string{"foo", "bar"})), Equals, true)

	otherErr = snap.AlreadyInstalledError{
		Components: map[string][]string{"other-snap": {"comp2", "comp1"}},
	}
	c.Check(errors.Is(err, otherErr), Equals, false)

	// check that snap.NewAlreadyInstalledComponentsError sorts the snaps in the resulting error
	c.Check(errors.Is(otherErr, snap.NewAlreadyInstalledComponentsError("other-snap", []string{"comp1", "comp2"})), Equals, false)
	otherErr = snap.AlreadyInstalledError{
		Components: map[string][]string{"other-snap": {"comp1", "comp2"}},
	}
	c.Check(errors.Is(otherErr, *snap.NewAlreadyInstalledComponentsError("other-snap", []string{"comp1", "comp2"})), Equals, true)

}

func (s *errorsSuite) TestNotSnapErrorNoDetails(c *C) {
	err := snap.NotSnapError{Path: "some-path"}
	c.Check(err, ErrorMatches, `cannot process snap or snapdir "some-path"`)
}

func (s *errorsSuite) TestNotSnapErrorWithDetails(c *C) {
	err := snap.NotSnapError{Path: "some-path", Err: fmt.Errorf(`cannot open "some path"`)}
	c.Check(err, ErrorMatches, `cannot process snap or snapdir: cannot open "some path"`)
}

func (s *errorsSuite) TestComponentNotInstalledErrorDetails(c *C) {
	err := snap.ComponentNotInstalledError{
		NotInstalledError: snap.NotInstalledError{Snap: "mysnap", Rev: snap.R(1)},
		Component:         "mycomp",
		CompRev:           snap.R(7),
	}
	c.Check(err, ErrorMatches,
		`revision 7 of component "mycomp" is not installed for revision 1 of snap "mysnap"`)

	err = snap.ComponentNotInstalledError{
		NotInstalledError: snap.NotInstalledError{Snap: "mysnap", Rev: snap.R(1)},
		Component:         "mycomp",
	}
	c.Check(err, ErrorMatches, `component "mycomp" is not installed for revision 1 of snap "mysnap"`)
}

func (s *errorsSuite) TestNotInstalledErrorIs(c *C) {
	err := &snap.NotInstalledError{Snap: "foo", Rev: snap.R(33)}
	c.Check(errors.Is(err, &snap.NotInstalledError{}), Equals, true)
	c.Check(errors.Is(errors.New("some error"), &snap.NotInstalledError{}), Equals, false)
}
