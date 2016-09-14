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
	"os"
	"os/user"
	"sort"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
)

func Test(t *testing.T) { TestingT(t) }

type HTestSuite struct{}

var _ = Suite(&HTestSuite{})

var mockYaml = []byte(`name: snapname
version: 1.0
apps:
 app:
  command: run-app
hooks:
 apply-config:
`)

var mockSnapInfo = &snap.Info{
	SuggestedName: "foo",
	Version:       "1.0",
	SideInfo: snap.SideInfo{
		Revision: snap.R(17),
	},
}

func (ts *HTestSuite) TestBasic(c *C) {
	env := basicEnv(mockSnapInfo)
	sort.Strings(env)

	c.Assert(env, DeepEquals, []string{
		fmt.Sprintf("SNAP=%s/foo/17", dirs.SnapMountDir),
		fmt.Sprintf("SNAP_ARCH=%s", arch.UbuntuArchitecture()),
		"SNAP_COMMON=/var/snap/foo/common",
		"SNAP_DATA=/var/snap/foo/17",
		"SNAP_LIBRARY_PATH=/var/lib/snapd/lib/gl:",
		"SNAP_NAME=foo",
		"SNAP_REEXEC=",
		"SNAP_REVISION=17",
		"SNAP_VERSION=1.0",
	})

}

func (ts *HTestSuite) TestUser(c *C) {
	env := userEnv(mockSnapInfo, "/root")
	sort.Strings(env)

	c.Assert(env, DeepEquals, []string{
		"HOME=/root/snap/foo/17",
		"SNAP_USER_COMMON=/root/snap/foo/common",
		"SNAP_USER_DATA=/root/snap/foo/17",
	})
}

func (s *HTestSuite) TestSnapRunSnapExecEnv(c *C) {
	info, err := snap.InfoFromSnapYaml(mockYaml)
	c.Assert(err, IsNil)
	info.SideInfo.Revision = snap.R(42)

	usr, err := user.Current()
	c.Assert(err, IsNil)

	homeEnv := os.Getenv("HOME")
	defer os.Setenv("HOME", homeEnv)

	for _, withHomeEnv := range []bool{true, false} {
		if !withHomeEnv {
			os.Setenv("HOME", "")
		}

		env := snapEnv(info)
		sort.Strings(env)
		c.Check(env, DeepEquals, []string{
			fmt.Sprintf("HOME=%s/snap/snapname/42", usr.HomeDir),
			fmt.Sprintf("SNAP=%s/snapname/42", dirs.SnapMountDir),
			fmt.Sprintf("SNAP_ARCH=%s", arch.UbuntuArchitecture()),
			"SNAP_COMMON=/var/snap/snapname/common",
			"SNAP_DATA=/var/snap/snapname/42",
			"SNAP_LIBRARY_PATH=/var/lib/snapd/lib/gl:",
			"SNAP_NAME=snapname",
			"SNAP_REEXEC=",
			"SNAP_REVISION=42",
			fmt.Sprintf("SNAP_USER_COMMON=%s/snap/snapname/common", usr.HomeDir),
			fmt.Sprintf("SNAP_USER_DATA=%s/snap/snapname/42", usr.HomeDir),
			"SNAP_VERSION=1.0",
		})
	}
}
