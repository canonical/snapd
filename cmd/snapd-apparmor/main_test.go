// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package main_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	snapd_apparmor "github.com/snapcore/snapd/cmd/snapd-apparmor"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type mainSuite struct {
	testutil.BaseTest
}

var _ = Suite(&mainSuite{})

func (s *mainSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

}

func (s *mainSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *mainSuite) TestIsContainerWithInternalPolicy(c *C) {
	c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, false)

	appArmorSecurityFSPath := filepath.Join(dirs.GlobalRootDir, "/sys/kernel/security/apparmor/")
	if err := os.MkdirAll(appArmorSecurityFSPath, 0755); err != nil {
		panic(err)
	}

	c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, false)

	// simulate being inside a container environment
	testutil.MockCommand(c, "systemd-detect-virt", "")
	c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, false)

	f, err := os.Create(filepath.Join(appArmorSecurityFSPath, ".ns_stacked"))
	if err != nil {
		panic(err)
	}
	f.WriteString("yes")
	f.Close()
	c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, false)

	f, err = os.Create(filepath.Join(appArmorSecurityFSPath, ".ns_name"))
	if err != nil {
		panic(err)
	}
	defer f.Close()
	c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, false)

	f.WriteString("foo")
	c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, false)
	// lxc/lxd name should result in a container with internal policy
	f.Seek(0, 0)
	f.WriteString("lxc-foo")
	c.Assert(snapd_apparmor.IsContainerWithInternalPolicy(), Equals, true)
}

func (s *mainSuite) TestLoadAppArmorProfiles(c *C) {
	testutil.MockCommand(c, "apparmor_parser", "")
	err := snapd_apparmor.LoadAppArmorProfiles()
	c.Assert(err, IsNil)

	// mock a profile
	if err = os.MkdirAll(dirs.SnapAppArmorDir, 0755); err != nil {
		panic(err)
	}

	profile := filepath.Join(dirs.SnapAppArmorDir, "foo")
	f, err := os.Create(profile)
	if err != nil {
		panic(err)
	}
	f.Close()

	err = snapd_apparmor.LoadAppArmorProfiles()
	c.Assert(err, IsNil)

	// catch unexpected changes to apparmor_parser compiler flags etc
	testutil.MockCommand(c, "apparmor_parser", `echo "$@"; ""exit "$#"`)
	err = snapd_apparmor.LoadAppArmorProfiles()
	c.Check(err.Error(), Equals, fmt.Sprintf("cannot load apparmor profiles: exit status 7\napparmor_parser output:\n--replace --write-cache -O no-expr-simplify --cache-loc=%s/var/cache/apparmor --quiet %s\n", dirs.GlobalRootDir, profile))

	// rename so file is ignored
	err = os.Rename(profile, profile+"~")
	if err != nil {
		panic(err)
	}
	err = snapd_apparmor.LoadAppArmorProfiles()
	c.Assert(err, IsNil)
}
