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
	c.Assert(os.WriteFile(filename, nil, 0644), check.IsNil)
	testCheck(c, FilePresent, true, "", filename)
}

func (*filePresenceCheckerSuite) TestFileAbsent(c *check.C) {
	d := c.MkDir()
	filename := filepath.Join(d, "foo")
	testInfo(c, FileAbsent, "FileAbsent", []string{"filename"})
	testCheck(c, FileAbsent, false, `filename must be a string`, 42)
	testCheck(c, FileAbsent, true, "", filename)
	c.Assert(os.WriteFile(filename, nil, 0644), check.IsNil)
	testCheck(c, FileAbsent, false, fmt.Sprintf(`file %q is present but should not exist`, filename), filename)
}
