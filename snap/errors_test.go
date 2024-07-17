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
