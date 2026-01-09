// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2018 Canonical Ltd
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
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/check.v1"

	. "github.com/snapcore/snapd/testutil"
)

type filePresenceCheckerSuite struct{}

var _ = check.Suite(&filePresenceCheckerSuite{})

func (*filePresenceCheckerSuite) TestFilePresent(c *check.C) {
	d := c.MkDir()
	filename := filepath.Join(d, "foo")
	testInfo(c, FilePresent, "FilePresent", []string{"filename"})
	testCheck(c, FilePresent, false, `filename must be a string`, 42)
	testCheck(c, FilePresent, false, fmt.Sprintf(`file %q is absent but should exist`, filename), filename)

	// Symlink is followed, file not present
	c.Assert(os.Symlink("bar", filename), check.IsNil)
	testCheck(c, FilePresent, false, fmt.Sprintf(`file %q is absent but should exist`, filename), filename)

	// Now use regular file
	c.Assert(os.Remove(filename), check.IsNil)
	c.Assert(os.WriteFile(filename, nil, 0o644), check.IsNil)
	testCheck(c, FilePresent, true, "", filename)
}

func (*filePresenceCheckerSuite) TestSymlinkPresent(c *check.C) {
	d := c.MkDir()
	filename := filepath.Join(d, "foo")
	testInfo(c, LFilePresent, "LFilePresent", []string{"filename"})
	testCheck(c, LFilePresent, false, `filename must be a string`, 42)
	testCheck(c, LFilePresent, false, fmt.Sprintf(`file %q is absent but should exist`, filename), filename)
	c.Assert(os.Symlink("bar", filename), check.IsNil)
	testCheck(c, LFilePresent, true, "", filename)
}

func (*filePresenceCheckerSuite) TestFileAbsent(c *check.C) {
	d := c.MkDir()
	filename := filepath.Join(d, "foo")
	testInfo(c, FileAbsent, "FileAbsent", []string{"filename"})
	testCheck(c, FileAbsent, false, `filename must be a string`, 42)
	testCheck(c, FileAbsent, true, "", filename)

	// Symlink is followed, still file is absent
	c.Assert(os.Symlink("bar", filename), check.IsNil)
	testCheck(c, FileAbsent, true, "", filename)

	// Now use regular file
	c.Assert(os.Remove(filename), check.IsNil)
	c.Assert(os.WriteFile(filename, nil, 0o644), check.IsNil)
	testCheck(c, FileAbsent, false, fmt.Sprintf(`file %q is present but should not exist`, filename), filename)
}

func (*filePresenceCheckerSuite) TestSymlinkAbsent(c *check.C) {
	d := c.MkDir()
	filename := filepath.Join(d, "foo")
	testInfo(c, LFileAbsent, "LFileAbsent", []string{"filename"})
	testCheck(c, LFileAbsent, false, `filename must be a string`, 42)
	testCheck(c, LFileAbsent, true, "", filename)
	c.Assert(os.Symlink("bar", filename), check.IsNil)
	testCheck(c, LFileAbsent, false, fmt.Sprintf(`file %q is present but should not exist`, filename), filename)
}
