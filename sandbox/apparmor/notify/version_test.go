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

package notify_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/testutil"
)

type versionSuite struct{}

var _ = Suite(&versionSuite{})

func (s *versionSuite) TestVersionsAndSupportedChecks(c *C) {
	// Check both directions so we get pretty printing for the values on error
	c.Check(notify.Versions, HasLen, len(notify.VersionLikelySupportedChecks))
	c.Check(notify.VersionLikelySupportedChecks, HasLen, len(notify.Versions))

	for _, ver := range notify.Versions {
		checkFn, exists := notify.VersionLikelySupportedChecks[ver]
		c.Check(exists, Equals, true, Commentf("version in versions missing from versionLikelySupportedChecks: %v", ver))
		c.Check(checkFn, NotNil, Commentf("version has nil supported check: %v", ver))
	}

	for ver, checkFn := range notify.VersionLikelySupportedChecks {
		c.Check(checkFn, NotNil, Commentf("version has nil supported check: %v", ver))
		c.Check(notify.Versions, testutil.Contains, ver, Commentf("version in versionLikelySupportedChecks missing from versions: %v", ver))
	}
}

var fakeVersions = []notify.VersionAndCheck{
	{
		Version: 2,
		Check:   func() bool { return false },
	},
	{
		Version: 3,
		Check:   func() bool { return true },
	},
	{
		Version: 5,
		Check:   func() bool { return false },
	},
	{
		Version: 7,
		Check:   func() bool { return true },
	},
	{
		Version: 11,
		Check:   func() bool { return false },
	},
}

func (s *versionSuite) TestVersionLikelySupported(c *C) {
	restore := notify.MockVersionLikelySupportedChecks(fakeVersions)
	defer restore()

	supported, err := notify.LikelySupported(notify.ProtocolVersion(1))
	c.Check(err, ErrorMatches, "internal error: no support check function defined for version .*")
	c.Check(supported, Equals, false)

	supported, err = notify.LikelySupported(notify.ProtocolVersion(2))
	c.Check(err, IsNil)
	c.Check(supported, Equals, false)

	supported, err = notify.LikelySupported(notify.ProtocolVersion(3))
	c.Check(err, IsNil)
	c.Check(supported, Equals, true)

	supported, err = notify.LikelySupported(notify.ProtocolVersion(4))
	c.Check(err, ErrorMatches, "internal error: no support check function defined for version .*")
	c.Check(supported, Equals, false)
}

func (s *versionSuite) TestLikelySupportedProtocolVersion(c *C) {
	restore := notify.MockVersionLikelySupportedChecks(fakeVersions)
	defer restore()

	for _, testCase := range []struct {
		unsupported       map[notify.ProtocolVersion]bool
		expectedVersion   notify.ProtocolVersion
		expectedSupported bool
		expectedMutated   map[notify.ProtocolVersion]bool
	}{
		{
			unsupported:       map[notify.ProtocolVersion]bool{},
			expectedVersion:   notify.ProtocolVersion(3),
			expectedSupported: true,
			expectedMutated:   map[notify.ProtocolVersion]bool{notify.ProtocolVersion(2): true},
		},
		{
			unsupported: map[notify.ProtocolVersion]bool{
				notify.ProtocolVersion(4): true,
				notify.ProtocolVersion(5): true,
			},
			expectedVersion:   notify.ProtocolVersion(3),
			expectedSupported: true,
			expectedMutated: map[notify.ProtocolVersion]bool{
				notify.ProtocolVersion(2): true,
				notify.ProtocolVersion(4): true,
				notify.ProtocolVersion(5): true,
			},
		},
		{
			unsupported: map[notify.ProtocolVersion]bool{
				notify.ProtocolVersion(3): true,
				notify.ProtocolVersion(4): true,
				notify.ProtocolVersion(5): true,
			},
			expectedVersion:   notify.ProtocolVersion(7),
			expectedSupported: true,
			expectedMutated: map[notify.ProtocolVersion]bool{
				notify.ProtocolVersion(2): true,
				notify.ProtocolVersion(3): true,
				notify.ProtocolVersion(4): true,
				notify.ProtocolVersion(5): true,
			},
		},
		{
			unsupported: map[notify.ProtocolVersion]bool{
				notify.ProtocolVersion(3): true,
				notify.ProtocolVersion(4): true,
				notify.ProtocolVersion(5): true,
				notify.ProtocolVersion(7): true,
			},
			expectedVersion:   notify.ProtocolVersion(0),
			expectedSupported: false,
			expectedMutated: map[notify.ProtocolVersion]bool{
				notify.ProtocolVersion(2):  true,
				notify.ProtocolVersion(3):  true,
				notify.ProtocolVersion(4):  true,
				notify.ProtocolVersion(5):  true,
				notify.ProtocolVersion(7):  true,
				notify.ProtocolVersion(11): true,
			},
		},
	} {
		protoVersion, supported := notify.LikelySupportedProtocolVersion(testCase.unsupported)
		comment := Commentf("testCase: %+v", testCase)
		c.Check(protoVersion, Equals, testCase.expectedVersion, comment)
		c.Check(supported, Equals, testCase.expectedSupported, comment)
		c.Check(testCase.unsupported, DeepEquals, testCase.expectedMutated, comment)
	}
}
