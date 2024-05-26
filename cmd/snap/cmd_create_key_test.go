// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package main_test

import (
	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestCreateKeyInvalidCharacters(c *C) {
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"create-key", "a b"}))
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "key name \"a b\" is not valid; only ASCII letters, digits, and hyphens are allowed")
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}
