// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

type overlaySuite struct{}

var _ = Suite(&overlaySuite{})

func (s *overlaySuite) TestIsRootOverlay(c *C) {
	cases := []struct {
		mountinfo, fstab string
		overlay          bool
		errorPattern     string
	}{{
		// Errors from parsing mountinfo and fstab are propagated.
		mountinfo:    "bad syntax",
		errorPattern: "cannot parse .*/mountinfo.*, .*",
	}, {
		fstab:        "bad syntax",
		errorPattern: "cannot parse .*/fstab.*, .*",
	}, {
		// overlay mounted on / are recognized
		mountinfo: "31 1 0:26 / / rw,relatime shared:1 - overlay /cow rw,lowerdir=//filesystem.squashfs,upperdir=/cow/upper,workdir=/cow/work",
		overlay:   true,
	}, {
		// overlay mounted elsewhere are ignored
		mountinfo: "31 1 0:26 /elsewhere /elsewhere rw,relatime shared:1 - overlay /cow rw,lowerdir=//filesystem.squashfs,upperdir=/cow/upper,workdir=/cow/work",
	}, {
		// overlay mounted at / is recognized
		fstab:   "overlay / overlay rw 0 0",
		overlay: true,
	}, {
		// overlay mounted elsewhere are ignored
		fstab: "overlay /elsewhere overlay rw 0 0",
	}}
	for _, tc := range cases {
		restore := osutil.MockMountInfo(tc.mountinfo)
		defer restore()
		restore = osutil.MockEtcFstab(tc.fstab)
		defer restore()

		overlay, err := osutil.IsRootOverlay()
		if tc.errorPattern != "" {
			c.Assert(err, ErrorMatches, tc.errorPattern, Commentf("test case %#v", tc))
		} else {
			c.Assert(err, IsNil)
		}
		c.Assert(overlay, Equals, tc.overlay)
	}
}
