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

func (s *errorsSuite) TestNotInstalledErrorIs(c *C) {
	err := snap.NotInstalledError{}
	c.Check(err.Is(&snap.NotInstalledError{}), Equals, true)
	c.Check(err.Is(errors.New("some error")), Equals, false)
}
