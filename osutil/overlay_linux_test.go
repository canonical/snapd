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

func (s *overlaySuite) TestIsRootWritableOverlay(c *C) {
	cases := []struct {
		mountinfo    string
		overlay      string
		errorPattern string
	}{{
		// Errors from parsing mountinfo are propagated.
		mountinfo:    "bad syntax",
		errorPattern: "cannot parse .*/mountinfo.*, .*",
	}, {
		// overlay mounted on / are recognized
		// casper mount source /cow
		mountinfo: "31 1 0:26 / / rw,relatime shared:1 - overlay /cow rw,lowerdir=//filesystem.squashfs,upperdir=/cow/upper,workdir=/cow/work",
		overlay:   "/upper",
	}, {
		// casper mount source upperdir trailing slash
		mountinfo: "31 1 0:26 / / rw,relatime shared:1 - overlay /cow rw,lowerdir=//filesystem.squashfs,upperdir=/cow/upper/,workdir=/cow/work",
		overlay:   "/upper",
	}, {
		// casper mount source trailing slash
		mountinfo: "31 1 0:26 / / rw,relatime shared:1 - overlay /cow/ rw,lowerdir=//filesystem.squashfs,upperdir=/cow/upper,workdir=/cow/work",
		overlay:   "/upper",
	}, {
		// non-casper mount source
		mountinfo: "31 1 0:26 / / rw,relatime shared:1 - overlay overlay rw,lowerdir=//filesystem.squashfs,upperdir=/cow/upper,workdir=/cow/work",
		overlay:   "/cow/upper",
	}, {
		// overlay mounted elsewhere are ignored
		mountinfo: "31 1 0:26 /elsewhere /elsewhere rw,relatime shared:1 - overlay /cow rw,lowerdir=//filesystem.squashfs,upperdir=/cow/upper,workdir=/cow/work",
	}, {
		mountinfo: "31 1 0:26 /elsewhere /elsewhere rw,relatime shared:1 - overlay overlay rw,lowerdir=//filesystem.squashfs,upperdir=/cow/upper,workdir=/cow/work",
	}, {
		// casper overlay which results in empty upperdir are ignored
		mountinfo: "31 1 0:26 / / rw,relatime shared:1 - overlay /upper rw,lowerdir=//filesystem.squashfs,upperdir=/upper,workdir=/cow/work",
	}, {
		// overlay with relative paths, AARE or double quotes are
		// ignored
		mountinfo: "31 1 0:26 / / rw,relatime shared:1 - overlay /cow rw,lowerdir=//filesystem.squashfs,upperdir=cow/upper,workdir=/cow/work",
	}, {
		mountinfo: "31 1 0:26 / / rw,relatime shared:1 - overlay /cow rw,lowerdir=//filesystem.squashfs,upperdir=/cow/bad?upper,workdir=/cow/work",
	}, {
		mountinfo: "31 1 0:26 / / rw,relatime shared:1 - overlay /cow rw,lowerdir=//filesystem.squashfs,upperdir=/cow/bad*upper,workdir=/cow/work",
	}, {
		mountinfo: "31 1 0:26 / / rw,relatime shared:1 - overlay /cow rw,lowerdir=//filesystem.squashfs,upperdir=/cow/bad[upper,workdir=/cow/work",
	}, {
		mountinfo: "31 1 0:26 / / rw,relatime shared:1 - overlay overlay rw,lowerdir=//filesystem.squashfs,upperdir=/cow/bad]upper,workdir=/cow/work",
	}, {
		mountinfo: "31 1 0:26 / / rw,relatime shared:1 - overlay overlay rw,lowerdir=//filesystem.squashfs,upperdir=/cow/bad{upper,workdir=/cow/work",
	}, {
		mountinfo: "31 1 0:26 / / rw,relatime shared:1 - overlay overlay rw,lowerdir=//filesystem.squashfs,upperdir=/cow/bad}upper,workdir=/cow/work",
	}, {
		mountinfo: "31 1 0:26 / / rw,relatime shared:1 - overlay overlay rw,lowerdir=//filesystem.squashfs,upperdir=/cow/bad^upper,workdir=/cow/work",
	}, {
		mountinfo: "31 1 0:26 / / rw,relatime shared:1 - overlay overlay rw,lowerdir=//filesystem.squashfs,upperdir=/cow/bad\"upper,workdir=/cow/work",
	}}
	for _, tc := range cases {
		restore := osutil.MockMountInfo(tc.mountinfo)
		defer restore()

		overlay, err := osutil.IsRootWritableOverlay()
		if tc.errorPattern != "" {
			c.Assert(err, ErrorMatches, tc.errorPattern, Commentf("test case %#v", tc))
		} else {
			c.Assert(err, IsNil)
		}
		c.Assert(overlay, Equals, tc.overlay)
	}
}
