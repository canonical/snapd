// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
package selinux_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/sandbox/selinux"
)

func Test(t *testing.T) { check.TestingT(t) }

type selinuxSuite struct{}

var _ = check.Suite(&selinuxSuite{})

const selinuxMountInfo = `90 0 252:1 / / rw,relatime shared:1 - ext4 /dev/vda1 rw,seclabel
41 19 0:18 / /sys/fs/selinux rw,relatime shared:20 - selinuxfs selinuxfs rw
42 21 0:17 / /dev/mqueue rw,relatime shared:26 - mqueue mqueue rw,seclabel
`

func (s *selinuxSuite) TestGetMount(c *check.C) {
	_, restore := selinux.MockMountInfo(c, selinuxMountInfo)
	defer restore()

	mnt, err := selinux.GetSELinuxMount()
	c.Assert(err, check.IsNil)
	c.Assert(mnt, check.Equals, "/sys/fs/selinux")
}

func (s *selinuxSuite) TestIsEnabledHappyEnabled(c *check.C) {
	_, restore := selinux.MockMountInfo(c, selinuxMountInfo)
	defer restore()

	enabled, err := selinux.IsEnabled()
	c.Assert(err, check.IsNil)
	c.Assert(enabled, check.Equals, true)
}

func (s *selinuxSuite) TestIsEnabledHappyNoSelinux(c *check.C) {
	_, restore := selinux.MockMountInfo(c, ``)
	defer restore()

	enabled, err := selinux.IsEnabled()
	c.Assert(err, check.IsNil)
	c.Assert(enabled, check.Equals, false)
}

func (s *selinuxSuite) TestIsEnabledFailMountInfo(c *check.C) {
	mi, restore := selinux.MockMountInfo(c, ``)
	defer restore()
	err := os.Chmod(mi, 0000)
	c.Assert(err, check.IsNil)

	enabled, err := selinux.IsEnabled()
	c.Assert(err, check.ErrorMatches, `failed to obtain SELinux mount path: .*`)
	c.Assert(enabled, check.Equals, false)
}

func (s *selinuxSuite) TestIsEnabledFailGarbage(c *check.C) {
	_, restore := selinux.MockMountInfo(c, `garbage`)
	defer restore()

	enabled, err := selinux.IsEnabled()
	c.Assert(err, check.ErrorMatches, `failed to obtain SELinux mount path: .*`)
	c.Assert(enabled, check.Equals, false)
}

func (s *selinuxSuite) TestIsEnforcingHappy(c *check.C) {
	dir := c.MkDir()
	miLine := fmt.Sprintf("41 19 0:18 / %s rw,relatime shared:20 - selinuxfs selinuxfs rw\n", dir)
	_, restore := selinux.MockMountInfo(c, miLine)
	defer restore()

	enforcePath := filepath.Join(dir, "enforce")

	err := ioutil.WriteFile(enforcePath, []byte("1"), 0644)
	c.Assert(err, check.IsNil)

	enforcing, err := selinux.IsEnforcing()
	c.Assert(err, check.IsNil)
	c.Assert(enforcing, check.Equals, true)

	err = ioutil.WriteFile(enforcePath, []byte("0"), 0644)
	c.Assert(err, check.IsNil)

	enforcing, err = selinux.IsEnforcing()
	c.Assert(err, check.IsNil)
	c.Assert(enforcing, check.Equals, false)
}

func (s *selinuxSuite) TestIsEnforcingNoSELinux(c *check.C) {
	_, restore := selinux.MockMountInfo(c, ``)
	defer restore()

	enforcing, err := selinux.IsEnforcing()
	c.Assert(err, check.IsNil)
	c.Assert(enforcing, check.Equals, false)
}

func (s *selinuxSuite) TestIsEnforcingFailGarbage(c *check.C) {
	dir := c.MkDir()
	miLine := fmt.Sprintf("41 19 0:18 / %s rw,relatime shared:20 - selinuxfs selinuxfs rw\n", dir)
	_, restore := selinux.MockMountInfo(c, miLine)
	defer restore()

	enforcePath := filepath.Join(dir, "enforce")

	err := ioutil.WriteFile(enforcePath, []byte("garbage"), 0644)
	c.Assert(err, check.IsNil)

	enforcing, err := selinux.IsEnforcing()
	c.Assert(err, check.ErrorMatches, "unknown SELinux status: garbage")
	c.Assert(enforcing, check.Equals, false)
}

func (s *selinuxSuite) TestIsEnforcingFailOther(c *check.C) {
	dir := c.MkDir()
	miLine := fmt.Sprintf("41 19 0:18 / %s rw,relatime shared:20 - selinuxfs selinuxfs rw\n", dir)
	_, restore := selinux.MockMountInfo(c, miLine)
	defer restore()

	enforcePath := filepath.Join(dir, "enforce")

	err := ioutil.WriteFile(enforcePath, []byte("not-readable"), 0000)
	c.Assert(err, check.IsNil)

	enforcing, err := selinux.IsEnforcing()
	c.Assert(err, check.ErrorMatches, "open .*: permission denied")
	c.Assert(enforcing, check.Equals, false)
}
