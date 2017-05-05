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
 configure:
`)

var mockSnapInfo = &snap.Info{
	SuggestedName: "foo",
	Version:       "1.0",
	SideInfo: snap.SideInfo{
		Revision: snap.R(17),
	},
}
var mockClassicSnapInfo = &snap.Info{
	SuggestedName: "foo",
	Version:       "1.0",
	SideInfo: snap.SideInfo{
		Revision: snap.R(17),
	},
	Confinement: snap.ClassicConfinement,
}

func (ts *HTestSuite) TestBasic(c *C) {
	env := basicEnv(mockSnapInfo)

	c.Assert(env, DeepEquals, map[string]string{
		"SNAP":              fmt.Sprintf("%s/foo/17", dirs.SnapMountDir),
		"SNAP_ARCH":         arch.UbuntuArchitecture(),
		"SNAP_COMMON":       "/var/snap/foo/common",
		"SNAP_DATA":         "/var/snap/foo/17",
		"SNAP_LIBRARY_PATH": "/var/lib/snapd/lib/gl:/var/lib/snapd/void",
		"SNAP_NAME":         "foo",
		"SNAP_REEXEC":       "",
		"SNAP_REVISION":     "17",
		"SNAP_VERSION":      "1.0",
	})

}

func (ts *HTestSuite) TestUser(c *C) {
	env := userEnv(mockSnapInfo, "/root")

	c.Assert(env, DeepEquals, map[string]string{
		"HOME":             "/root/snap/foo/17",
		"SNAP_USER_COMMON": "/root/snap/foo/common",
		"SNAP_USER_DATA":   "/root/snap/foo/17",
		"XDG_RUNTIME_DIR":  fmt.Sprintf("/run/user/%d/snap.foo", os.Geteuid()),
	})
}

func (ts *HTestSuite) TestUserForClassicConfinement(c *C) {
	env := userEnv(mockClassicSnapInfo, "/root")

	c.Assert(env, DeepEquals, map[string]string{
		// NOTE HOME Is absent! we no longer override it
		"SNAP_USER_COMMON": "/root/snap/foo/common",
		"SNAP_USER_DATA":   "/root/snap/foo/17",
		"XDG_RUNTIME_DIR":  fmt.Sprintf("/run/user/%d/snap.foo", os.Geteuid()),
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
		c.Check(env, DeepEquals, map[string]string{
			"HOME":              fmt.Sprintf("%s/snap/snapname/42", usr.HomeDir),
			"SNAP":              fmt.Sprintf("%s/snapname/42", dirs.SnapMountDir),
			"SNAP_ARCH":         arch.UbuntuArchitecture(),
			"SNAP_COMMON":       "/var/snap/snapname/common",
			"SNAP_DATA":         "/var/snap/snapname/42",
			"SNAP_LIBRARY_PATH": "/var/lib/snapd/lib/gl:/var/lib/snapd/void",
			"SNAP_NAME":         "snapname",
			"SNAP_REEXEC":       "",
			"SNAP_REVISION":     "42",
			"SNAP_USER_COMMON":  fmt.Sprintf("%s/snap/snapname/common", usr.HomeDir),
			"SNAP_USER_DATA":    fmt.Sprintf("%s/snap/snapname/42", usr.HomeDir),
			"SNAP_VERSION":      "1.0",
			"XDG_RUNTIME_DIR":   fmt.Sprintf("/run/user/%d/snap.snapname", os.Geteuid()),
		})
	}
}

func (s *HTestSuite) TestExtraEnvForExecEnv(c *C) {
	info, err := snap.InfoFromSnapYaml(mockYaml)
	c.Assert(err, IsNil)
	info.SideInfo.Revision = snap.R(42)

	env := ExecEnv(info, map[string]string{"FOO": "BAR"})
	found := false
	for _, item := range env {
		if item == "FOO=BAR" {
			found = true
			break
		}
	}
	c.Assert(found, Equals, true)
}
