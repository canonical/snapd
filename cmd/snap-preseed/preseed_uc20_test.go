// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-preseed"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/image/preseed"
)

func (s *startPreseedSuite) TestRunPreseedUC20Happy(c *C) {
	tmpDir := c.MkDir()
	dirs.SetRootDir(tmpDir)

	restore := main.MockOsGetuid(func() int {
		return 0
	})
	defer restore()

	// for UC20 probing
	c.Assert(os.MkdirAll(filepath.Join(tmpDir, "system-seed/systems/20220203"), 0755), IsNil)
	// we don't run tar, so create a fake artifact to make FileDigest happy
	c.Assert(os.WriteFile(filepath.Join(tmpDir, "system-seed/systems/20220203/preseed.tgz"), nil, 0644), IsNil)

	var called bool
	restorePreseed := main.MockPreseedCore20(func(opts *preseed.CoreOptions) error {
		c.Check(opts.PrepareImageDir, Equals, tmpDir)
		c.Check(opts.PreseedSignKey, Equals, "key")
		c.Check(opts.AppArmorKernelFeaturesDir, Equals, "/custom/aa/features")
		c.Check(opts.SysfsOverlay, Equals, "/sysfs-overlay")
		called = true
		return nil
	})
	defer restorePreseed()

	parser := testParser(c)
	c.Assert(main.Run(parser, []string{"--preseed-sign-key", "key", "--apparmor-features-dir", "/custom/aa/features", "--sysfs-overlay", "/sysfs-overlay", tmpDir}), IsNil)
	c.Check(called, Equals, true)
}

func (s *startPreseedSuite) TestRunPreseedUC20HappyNoArgs(c *C) {
	tmpDir := c.MkDir()
	dirs.SetRootDir(tmpDir)

	restore := main.MockOsGetuid(func() int {
		return 0
	})
	defer restore()

	// for UC20 probing
	c.Assert(os.MkdirAll(filepath.Join(tmpDir, "system-seed/systems/20220203"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(tmpDir, "system-seed/systems/20220203/preseed.tgz"), nil, 0644), IsNil)

	var called bool
	restorePreseed := main.MockPreseedCore20(func(opts *preseed.CoreOptions) error {
		c.Check(opts.PrepareImageDir, Equals, tmpDir)
		c.Check(opts.PreseedSignKey, Equals, "")
		c.Check(opts.AppArmorKernelFeaturesDir, Equals, "")
		c.Check(opts.SysfsOverlay, Equals, "")
		called = true
		return nil
	})
	defer restorePreseed()

	parser := testParser(c)
	c.Assert(main.Run(parser, []string{tmpDir}), IsNil)
	c.Check(called, Equals, true)
}

func (s *startPreseedSuite) TestResetUC20(c *C) {
	tmpDir := c.MkDir()
	dirs.SetRootDir(tmpDir)

	restore := main.MockOsGetuid(func() int {
		return 0
	})
	defer restore()

	// for UC20 probing
	c.Assert(os.MkdirAll(filepath.Join(tmpDir, "system-seed/systems/20220203"), 0755), IsNil)
	// we don't run tar, so create a fake artifact to make FileDigest happy
	c.Assert(os.WriteFile(filepath.Join(tmpDir, "system-seed/systems/20220203/preseed.tgz"), nil, 0644), IsNil)

	var called bool
	restorePreseed := main.MockPreseedCore20(func(opts *preseed.CoreOptions) error {
		called = true
		return nil
	})
	defer restorePreseed()

	parser := testParser(c)
	res := main.Run(parser, []string{"--reset", tmpDir})
	c.Assert(res, Not(IsNil))
	c.Check(res, ErrorMatches, "cannot snap-preseed --reset for Ubuntu Core")
	c.Check(called, Equals, false)
}
