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

package snapenv

import (
	"fmt"
	"sort"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/snap"
)

func Test(t *testing.T) { TestingT(t) }

type HTestSuite struct{}

var _ = Suite(&HTestSuite{})

var mockSnapInfo = &snap.Info{
	SuggestedName: "foo",
	Version:       "1.0",
	SideInfo: snap.SideInfo{
		Revision: snap.R(17),
	},
}

func (ts *HTestSuite) TestBasic(c *C) {
	env := Basic(mockSnapInfo)
	sort.Strings(env)

	c.Assert(env, DeepEquals, []string{
		"SNAP=/snap/foo/17",
		fmt.Sprintf("SNAP_ARCH=%s", arch.UbuntuArchitecture()),
		"SNAP_DATA=/var/snap/foo/17",
		"SNAP_LIBRARY_PATH=/var/lib/snapd/lib/gl:",
		"SNAP_NAME=foo",
		"SNAP_REVISION=17",
		"SNAP_VERSION=1.0",
	})

}

func (ts *HTestSuite) TestUser(c *C) {
	env := User(mockSnapInfo, "/root")
	c.Assert(env, DeepEquals, []string{
		"SNAP_USER_DATA=/root/snap/foo/17",
	})
}
