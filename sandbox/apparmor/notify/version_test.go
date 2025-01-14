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
)

type versionSuite struct{}

var _ = Suite(&versionSuite{})

func (s *versionSuite) TestVersionsAndCallbacks(c *C) {
	// Check both directions so we get pretty printing for the values on error
	c.Check(notify.Versions, HasLen, len(notify.VersionSupportedCallbacks))
	c.Check(notify.VersionSupportedCallbacks, HasLen, len(notify.Versions))

	for _, version := range notify.Versions {
		callback, exists := notify.VersionSupportedCallbacks[version]
		c.Check(exists, Equals, true, Commentf("version in versions missing from versionSupportedCallbacks: %v", version))
		c.Check(callback, NotNil, Commentf("version has nil callback: %v", version))
	}

	for version, callback := range notify.VersionSupportedCallbacks {
		c.Check(callback, NotNil, Commentf("version has nil callback: %v", version))
		found := false
		for _, v := range notify.Versions {
			if version == v {
				found = true
				break
			}
		}
		c.Check(found, Equals, true, Commentf("version in versionSupportedCallbacks missing from versions: %v", version))
	}
}

var fakeVersions = []notify.VersionAndCallback{
	{
		Version:  2,
		Callback: func() bool { return false },
	},
	{
		Version:  3,
		Callback: func() bool { return true },
	},
	{
		Version:  5,
		Callback: func() bool { return false },
	},
	{
		Version:  7,
		Callback: func() bool { return true },
	},
	{
		Version:  11,
		Callback: func() bool { return false },
	},
}

func (s *versionSuite) TestVersionSupported(c *C) {
	restore := notify.MockVersionSupportedCallbacks(fakeVersions)
	defer restore()

	supported, err := notify.Supported(notify.Version(1))
	c.Check(supported, Equals, false)
	c.Check(err, ErrorMatches, "no callback defined for version .*")

	supported, err = notify.Supported(notify.Version(2))
	c.Check(supported, Equals, false)
	c.Check(err, IsNil)

	supported, err = notify.Supported(notify.Version(3))
	c.Check(supported, Equals, true)
	c.Check(err, IsNil)

	supported, err = notify.Supported(notify.Version(4))
	c.Check(supported, Equals, false)
	c.Check(err, ErrorMatches, "no callback defined for version .*")
}

func (s *versionSuite) TestSupportedProtocolVersion(c *C) {
	restore := notify.MockVersionSupportedCallbacks(fakeVersions)
	defer restore()

	for _, testCase := range []struct {
		unsupported       map[notify.Version]bool
		expectedVersion   notify.Version
		expectedSupported bool
		expectedMutated   map[notify.Version]bool
	}{
		{
			unsupported:       map[notify.Version]bool{},
			expectedVersion:   notify.Version(3),
			expectedSupported: true,
			expectedMutated:   map[notify.Version]bool{notify.Version(2): true},
		},
		{
			unsupported: map[notify.Version]bool{
				notify.Version(4): true,
				notify.Version(5): true,
			},
			expectedVersion:   notify.Version(3),
			expectedSupported: true,
			expectedMutated: map[notify.Version]bool{
				notify.Version(2): true,
				notify.Version(4): true,
				notify.Version(5): true,
			},
		},
		{
			unsupported: map[notify.Version]bool{
				notify.Version(3): true,
				notify.Version(4): true,
				notify.Version(5): true,
			},
			expectedVersion:   notify.Version(7),
			expectedSupported: true,
			expectedMutated: map[notify.Version]bool{
				notify.Version(2): true,
				notify.Version(3): true,
				notify.Version(4): true,
				notify.Version(5): true,
			},
		},
		{
			unsupported: map[notify.Version]bool{
				notify.Version(3): true,
				notify.Version(4): true,
				notify.Version(5): true,
				notify.Version(7): true,
			},
			expectedVersion:   notify.Version(0),
			expectedSupported: false,
			expectedMutated: map[notify.Version]bool{
				notify.Version(2):  true,
				notify.Version(3):  true,
				notify.Version(4):  true,
				notify.Version(5):  true,
				notify.Version(7):  true,
				notify.Version(11): true,
			},
		},
	} {
		version, supported := notify.SupportedProtocolVersion(testCase.unsupported)
		c.Check(version, Equals, testCase.expectedVersion, Commentf("testCase: %+v", testCase))
		c.Check(supported, Equals, testCase.expectedSupported, Commentf("testCase: %+v", testCase))
		c.Check(testCase.unsupported, DeepEquals, testCase.expectedMutated, Commentf("testCase: %+v"))
	}
}
