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
	c.Assert(snapd_apparmor.IsContainerWithInternalLXDPolicy(), Equals, false)

	appArmorSecurityFSPath := filepath.Join(dirs.GlobalRootDir, "/sys/kernel/security/apparmor/")
	err := os.MkdirAll(appArmorSecurityFSPath, 0755)
	c.Assert(err, IsNil)

	c.Assert(snapd_apparmor.IsContainerWithInternalLXDPolicy(), Equals, false)

	// simulate being inside a container environment
	testutil.MockCommand(c, "systemd-detect-virt", "")
	c.Assert(snapd_apparmor.IsContainerWithInternalLXDPolicy(), Equals, false)

	f, err := os.Create(filepath.Join(appArmorSecurityFSPath, ".ns_stacked"))
	c.Assert(err, IsNil)
	f.WriteString("yes")
	f.Close()
	c.Assert(snapd_apparmor.IsContainerWithInternalLXDPolicy(), Equals, false)

	f, err = os.Create(filepath.Join(appArmorSecurityFSPath, ".ns_name"))
	c.Assert(err, IsNil)
	defer f.Close()
	c.Assert(snapd_apparmor.IsContainerWithInternalLXDPolicy(), Equals, false)

	f.WriteString("foo")
	c.Assert(snapd_apparmor.IsContainerWithInternalLXDPolicy(), Equals, false)
	// lxc/lxd name should result in a container with internal policy
	f.Seek(0, 0)
	f.WriteString("lxc-foo")
	c.Assert(snapd_apparmor.IsContainerWithInternalLXDPolicy(), Equals, true)
}

func (s *mainSuite) TestLoadAppArmorProfiles(c *C) {
	parserCmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer parserCmd.Restore()
	err := snapd_apparmor.LoadAppArmorProfiles()
	c.Assert(err, IsNil)

	// mock a profile
	err = os.MkdirAll(dirs.SnapAppArmorDir, 0755)
	c.Assert(err, IsNil)

	profile := filepath.Join(dirs.SnapAppArmorDir, "foo")
	f, err := os.Create(profile)
	c.Assert(err, IsNil)
	f.Close()

	err = snapd_apparmor.LoadAppArmorProfiles()
	c.Assert(err, IsNil)

	// check arguments to the parser are as expected
	c.Assert(parserCmd.Calls(), DeepEquals, [][]string{
		{"apparmor_parser", "--replace", "--write-cache",
			"-O", "no-expr-simplify",
			fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", dirs.GlobalRootDir),
			"--quiet", profile}})

	// test error case
	testutil.MockCommand(c, "apparmor_parser", "exit 1")
	err = snapd_apparmor.LoadAppArmorProfiles()
	c.Check(err.Error(), Equals, fmt.Sprintf("cannot load apparmor profiles: exit status 1\napparmor_parser output:\n"))

	// rename so file is ignored
	err = os.Rename(profile, profile+"~")
	c.Assert(err, IsNil)
	err = snapd_apparmor.LoadAppArmorProfiles()
	c.Assert(err, IsNil)
}

func (s *mainSuite) TestIsContainer(c *C) {
	c.Check(snapd_apparmor.IsContainer(), Equals, false)

	detectCmd := testutil.MockCommand(c, "systemd-detect-virt", "")
	defer detectCmd.Restore()

	c.Check(snapd_apparmor.IsContainer(), Equals, true)
	c.Assert(detectCmd.Calls(), DeepEquals, [][]string{
		{"systemd-detect-virt", "--quiet", "--container"}})
}
