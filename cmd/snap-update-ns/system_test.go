// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
)

type systemSuite struct{}

var _ = Suite(&systemSuite{})

func (s *systemSuite) TestDesiredSystemProfilePath(c *C) {
	c.Check(update.DesiredSystemProfilePath("foo"), Equals, "/var/lib/snapd/mount/snap.foo.fstab")
}

func (s *systemSuite) TestCurrentSystemProfilePath(c *C) {
	c.Check(update.CurrentSystemProfilePath("foo"), Equals, "/run/snapd/ns/snap.foo.fstab")
}
