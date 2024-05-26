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

package syscheck_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/syscheck"
)

func (s *syscheckSuite) TestCheckApparmorUsable(c *C) {
	epermProfilePath := filepath.Join(c.MkDir(), "profiles")
	restore := syscheck.MockAppArmorProfilesPath(epermProfilePath)
	defer restore()
	mylog.Check(os.Chmod(filepath.Dir(epermProfilePath), 0444))

	mylog.Check(syscheck.CheckApparmorUsable())
	c.Check(err, ErrorMatches, "apparmor detected but insufficient permissions to use it")
}
