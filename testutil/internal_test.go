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

package testutil_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
)

type internalSuite struct{}

var _ = Suite(&internalSuite{})

func (s *internalSuite) TestInternalCmdPath(c *C) {
	restore := testutil.MockInternalCmdPath("@LIBEXECDIR@")
	defer restore()
	c.Check(dirs.InternalCmdPath("snap-foo"), Equals, "@LIBEXECDIR@/snap-foo")
}

func (s *internalSuite) TestInternalCmdPathEmpty(c *C) {
	restore := testutil.MockInternalCmdPath("")
	defer restore()
	c.Check(dirs.InternalCmdPath("snap-foo"), Equals, "snap-foo")
}
