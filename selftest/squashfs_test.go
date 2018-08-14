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

	"github.com/snapcore/snapd/osutil/squashfs"
	"github.com/snapcore/snapd/selftest"
	"github.com/snapcore/snapd/testutil"
)

func (s *selftestSuite) TestTrySquasfsMountHappy(c *C) {
	restore := squashfs.MockUseFuse(false)
	defer restore()

	// we create a canary.txt with the same prefix as the real one
	mockMount := testutil.MockCommand(c, "mount", "echo 'This file is used to check that snapd can read a squashfs image.' > $4/canary.txt")
	defer mockMount.Restore()

	mockUmount := testutil.MockCommand(c, "umount", "")
	defer mockUmount.Restore()

	err := selftest.TrySquashfsMount()
	c.Check(err, IsNil)

	c.Check(mockMount.Calls(), HasLen, 1)
	c.Check(mockUmount.Calls(), HasLen, 1)

	squashfsFile := mockMount.Calls()[0][3]
	mountPoint := mockMount.Calls()[0][4]
	c.Check(mockMount.Calls(), DeepEquals, [][]string{
		{"mount", "-t", "squashfs", squashfsFile, mountPoint},
	})
	c.Check(mockUmount.Calls(), DeepEquals, [][]string{
		{"umount", mountPoint},
	})
}

func (s *selftestSuite) TestTrySquasfsMountNotHappy(c *C) {
	restore := squashfs.MockUseFuse(false)
	defer restore()

	mockMount := testutil.MockCommand(c, "mount", "echo iz-broken;false")
	defer mockMount.Restore()

	mockUmount := testutil.MockCommand(c, "umount", "")
	defer mockUmount.Restore()

	err := selftest.TrySquashfsMount()
	c.Check(err, ErrorMatches, "cannot mount squashfs image using.*")

	c.Check(mockMount.Calls(), HasLen, 1)
	c.Check(mockUmount.Calls(), HasLen, 0)

	squashfsFile := mockMount.Calls()[0][3]
	mountPoint := mockMount.Calls()[0][4]
	c.Check(mockMount.Calls(), DeepEquals, [][]string{
		{"mount", "-t", "squashfs", squashfsFile, mountPoint},
	})
}

func (s *selftestSuite) TestTrySquasfsMountWrongContent(c *C) {
	restore := squashfs.MockUseFuse(false)
	defer restore()

	mockMount := testutil.MockCommand(c, "mount", "echo 'wrong content' > $4/canary.txt")
	defer mockMount.Restore()

	mockUmount := testutil.MockCommand(c, "umount", "")
	defer mockUmount.Restore()

	err := selftest.TrySquashfsMount()
	c.Check(err, ErrorMatches, `unexpected squashfs canary content: "wrong content\\n"`)

	c.Check(mockMount.Calls(), HasLen, 1)
	c.Check(mockUmount.Calls(), HasLen, 1)
}
