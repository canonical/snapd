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

package snaplock_test

import (
	"os"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/cmd/snaplock"
	"github.com/snapcore/snapd/dirs"
)

func Test(t *testing.T) {
	TestingT(t)
}

type lockSuite struct{}

var _ = Suite(&lockSuite{})

func (s *lockSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *lockSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *lockSuite) TestOpenLock(c *C) {
	lock := mylog.Check2(snaplock.OpenLock("name"))

	defer lock.Close()

	_ = mylog.Check2(os.Stat(lock.Path()))


	comment := Commentf("wrong prefix for %q, want %q", lock.Path(), dirs.SnapRunLockDir)
	c.Check(strings.HasPrefix(lock.Path(), dirs.SnapRunLockDir), Equals, true, comment)
}
