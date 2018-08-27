// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package selftest_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/selftest"
)

func (s *selftestSuite) TestApparmorUsable(c *C) {
	epermProfilePath := filepath.Join(c.MkDir(), "profiles")
	restore := selftest.MockAppArmorProfilesPath(epermProfilePath)
	defer restore()
	err := os.Chmod(filepath.Dir(epermProfilePath), 0444)
	c.Assert(err, IsNil)

	err = selftest.ApparmorUsable()
	c.Check(err, ErrorMatches, "apparmor detected but insufficient permissions to use it")
}
