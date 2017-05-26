// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package mount_test

import (
	"os"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/mount"
)

type lockSuite struct{}

var _ = Suite(&lockSuite{})

func (s *lockSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *lockSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *lockSuite) TestOpenLock(c *C) {
	lock, err := mount.OpenLock("name")
	c.Assert(err, IsNil)
	defer lock.Close()

	_, err = os.Stat(lock.Path())
	c.Assert(err, IsNil)

	c.Check(strings.HasPrefix(lock.Path(), dirs.SnapRunLockDir), Equals, true, Commentf("wrong prefix for %q, want %q", lock.Path(), dirs.SnapRunLockDir))
}
