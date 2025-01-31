// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package snapstate_test

import (
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
)

type iconSuite struct{}

var _ = Suite(&iconSuite{})

func (s *iconSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *iconSuite) TestIconDownloadFilename(c *C) {
	filename := snapstate.IconDownloadFilename("some-snap-id")
	c.Check(filename, Equals, filepath.Join(dirs.SnapIconsPoolDir, "some-snap-id.icon"))
}

func (s *iconSuite) TestIconInstallFilename(c *C) {
	filename := snapstate.IconInstallFilename("some-snap-id")
	c.Check(filename, Equals, filepath.Join(dirs.SnapIconsDir, "some-snap-id.icon"))
}

func (s *iconSuite) TestSnapIconLinkUnlinkDiscardPermutations(c *C) {
	for i, testCase := range []struct {
		functions                []func(snapID string) error
		expectedErrors           []string
		poolIconExistsAfter      []bool
		installedIconExistsAfter []bool
	}{
		{
			functions:                []func(string) error{snapstate.LinkSnapIcon, snapstate.UnlinkSnapIcon, snapstate.DiscardSnapIcon},
			expectedErrors:           []string{"", "", ""},
			poolIconExistsAfter:      []bool{true, true, false},
			installedIconExistsAfter: []bool{true, false, false},
		},
		{
			functions:                []func(string) error{snapstate.LinkSnapIcon, snapstate.DiscardSnapIcon, snapstate.UnlinkSnapIcon},
			expectedErrors:           []string{"", "", ""},
			poolIconExistsAfter:      []bool{true, false, false},
			installedIconExistsAfter: []bool{true, true, false},
		},
		{
			functions:                []func(string) error{snapstate.UnlinkSnapIcon, snapstate.LinkSnapIcon, snapstate.DiscardSnapIcon},
			expectedErrors:           []string{"", "", ""},
			poolIconExistsAfter:      []bool{true, true, false},
			installedIconExistsAfter: []bool{false, true, true},
		},
		{
			functions:                []func(string) error{snapstate.UnlinkSnapIcon, snapstate.DiscardSnapIcon, snapstate.LinkSnapIcon},
			expectedErrors:           []string{"", "", ".*no such file or directory"}, // TODO: fix
			poolIconExistsAfter:      []bool{true, false, false},
			installedIconExistsAfter: []bool{false, false, false},
		},
		{
			functions:                []func(string) error{snapstate.DiscardSnapIcon, snapstate.LinkSnapIcon, snapstate.UnlinkSnapIcon},
			expectedErrors:           []string{"", ".*no such file or directory", ""}, // TODO: fix
			poolIconExistsAfter:      []bool{false, false, false},
			installedIconExistsAfter: []bool{false, false, false},
		},
		{
			functions:                []func(string) error{snapstate.DiscardSnapIcon, snapstate.UnlinkSnapIcon, snapstate.LinkSnapIcon},
			expectedErrors:           []string{"", "", ".*no such file or directory"}, // TODO: fix
			poolIconExistsAfter:      []bool{false, false, false},
			installedIconExistsAfter: []bool{false, false, false},
		},
	} {
		// consistency check on the test case itself
		c.Assert(testCase.functions, HasLen, len(testCase.expectedErrors))
		c.Assert(testCase.functions, HasLen, len(testCase.poolIconExistsAfter))
		c.Assert(testCase.functions, HasLen, len(testCase.installedIconExistsAfter))

		snapID := fmt.Sprintf("some-snap-id-%d", i)
		poolIconPath := snapstate.IconDownloadFilename(snapID)
		installedIconPath := snapstate.IconInstallFilename(snapID)

		// create the initial snap icon in the icon pool directory
		c.Assert(os.MkdirAll(dirs.SnapIconsPoolDir, 0o755), IsNil)
		fileContents := []byte("image data")
		c.Assert(os.WriteFile(poolIconPath, fileContents, 0o644), IsNil)

		for step, f := range testCase.functions {
			err := f(snapID)
			if errStr := testCase.expectedErrors[step]; errStr != "" {
				c.Check(err, ErrorMatches, errStr, Commentf("test case %d, step %d", i, step))
			} else {
				c.Check(err, IsNil, Commentf("test case %d, step %d", i, step))
			}
			c.Check(osutil.FileExists(poolIconPath), Equals, testCase.poolIconExistsAfter[step], Commentf("test case %d, step %d", i, step))
			c.Check(osutil.FileExists(installedIconPath), Equals, testCase.installedIconExistsAfter[step], Commentf("test case %d, step %d", i, step))
		}
	}
}
