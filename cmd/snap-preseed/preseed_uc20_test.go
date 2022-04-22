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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-preseed"
	"github.com/snapcore/snapd/dirs"
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
	c.Assert(ioutil.WriteFile(filepath.Join(tmpDir, "system-seed/systems/20220203/preseed.tgz"), nil, 0644), IsNil)

	var called bool
	restorePreseed := main.MockPreseedCore20(func(dir, key string) error {
		c.Check(dir, Equals, tmpDir)
		c.Check(key, Equals, "key")
		called = true
		return nil
	})
	defer restorePreseed()

	parser := testParser(c)
	c.Assert(main.Run(parser, []string{"--preseed-sign-key", "key", tmpDir}), IsNil)
	c.Check(called, Equals, true)
}
