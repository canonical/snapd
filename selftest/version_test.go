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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/selftest"
)

type versionSuite struct{}

var _ = Suite(&versionSuite{})

func (s *selftestSuite) TestFreshInstallOfSnapdOnTrusty(c *C) {
	// Mock an Ubuntu 14.04 system running a 3.13 kernel
	restore := release.MockOnClassic(true)
	defer restore()
	restore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer restore()
	restore = osutil.MockKernelVersion("3.13.0-35-generic")
	defer restore()

	// Check for the given advice.
	err := selftest.CheckKernelVersion()
	c.Assert(err, ErrorMatches, "you need to reboot into a 4.4 kernel to start using snapd")
}
