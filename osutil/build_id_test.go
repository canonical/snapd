// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package osutil_test

import (
	"github.com/snapcore/snapd/osutil"

	. "gopkg.in/check.v1"
)

type buildIDSuite struct{}

var _ = Suite(&buildIDSuite{})

func (s *buildIDSuite) TestString(c *C) {
	id1 := osutil.BuildID([]byte{0xef, 0xbf, 0xc, 0xe8, 0xdd, 0x96, 0x17, 0xc8, 0x90, 0xa0, 0x54, 0x7c, 0xe5, 0xa1, 0xa6, 0x7, 0x3f, 0x58, 0x67, 0xaf})
	c.Assert(id1.String(), Equals, "BuildID[sha1]=efbf0ce8dd9617c890a0547ce5a1a6073f5867af")

	id2 := osutil.BuildID([]byte{0xde, 0xad, 0xbe, 0xef})
	c.Assert(id2.String(), Equals, "BuildID[???]=deadbeef")
}

func (s *buildIDSuite) TestGetBuildID(c *C) {
	for _, t := range []struct {
		fname, expected string
	}{
		{"true.i386", "BuildID[sha1]=159364c90b873eb5def7431c2ee7d1385e58be51"},
		{"true.amd64", "BuildID[sha1]=efbf0ce8dd9617c890a0547ce5a1a6073f5867af"},
		{"true.arm64", "BuildID[sha1]=8b65339d7fa0c4cdc87ed9c8020626aa10fb521b"},
		{"true.armhf", "BuildID[sha1]=c80229c22d4b6b30b71ab1b1b5a1de6b86b6aadf"},
	} {
		id, err := osutil.GetBuildID(t.fname)
		c.Assert(err, IsNil)
		c.Assert(id.String(), Equals, t.expected, Commentf("executable: %s", t.fname))
	}
}

func (s *buildIDSuite) TestGetBuildIDNoID(c *C) {
	// The test file was processed to strip the section containing the build-id note
	id, err := osutil.GetBuildID("true.noid.amd64")
	c.Assert(err, Equals, osutil.ErrNoBuildID)
	c.Assert(id, IsNil)
}
