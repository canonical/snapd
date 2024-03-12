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
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
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
	restore := osutil.MockMountInfo(selinuxMountInfo)
	defer restore()

	mnt, err := selinux.GetSELinuxMount()
	c.Assert(err, check.IsNil)
	c.Assert(mnt, check.Equals, "/sys/fs/selinux")
}

func (s *selinuxSuite) TestIsEnabledHappyEnabled(c *check.C) {
	restore := osutil.MockMountInfo(selinuxMountInfo)
	defer restore()

	enabled, err := selinux.IsEnabled()
	c.Assert(err, check.IsNil)
	c.Assert(enabled, check.Equals, true)
}

func (s *selinuxSuite) TestIsEnabledHappyNoSelinux(c *check.C) {
	restore := osutil.MockMountInfo("")
	defer restore()

	enabled, err := selinux.IsEnabled()
	c.Assert(err, check.IsNil)
	c.Assert(enabled, check.Equals, false)
}

func (s *selinuxSuite) TestIsEnabledFailGarbage(c *check.C) {
	restore := osutil.MockMountInfo("garbage")
	defer restore()

	enabled, err := selinux.IsEnabled()
	c.Assert(err, check.ErrorMatches, `failed to obtain SELinux mount path: .*`)
	c.Assert(enabled, check.Equals, false)
}

func (s *selinuxSuite) TestIsEnforcingHappy(c *check.C) {
	dir := c.MkDir()
	miLine := fmt.Sprintf("41 19 0:18 / %s rw,relatime shared:20 - selinuxfs selinuxfs rw\n", dir)
	restore := osutil.MockMountInfo(miLine)
	defer restore()

	enforcePath := filepath.Join(dir, "enforce")

	err := os.WriteFile(enforcePath, []byte("1"), 0644)
	c.Assert(err, check.IsNil)

	enforcing, err := selinux.IsEnforcing()
	c.Assert(err, check.IsNil)
	c.Assert(enforcing, check.Equals, true)

	err = os.WriteFile(enforcePath, []byte("0"), 0644)
	c.Assert(err, check.IsNil)

	enforcing, err = selinux.IsEnforcing()
	c.Assert(err, check.IsNil)
	c.Assert(enforcing, check.Equals, false)
}

func (s *selinuxSuite) TestIsEnforcingNoSELinux(c *check.C) {
	restore := osutil.MockMountInfo("")
	defer restore()

	enforcing, err := selinux.IsEnforcing()
	c.Assert(err, check.IsNil)
	c.Assert(enforcing, check.Equals, false)
}

func (s *selinuxSuite) TestIsEnforcingFailGarbage(c *check.C) {
	dir := c.MkDir()
	miLine := fmt.Sprintf("41 19 0:18 / %s rw,relatime shared:20 - selinuxfs selinuxfs rw\n", dir)
	restore := osutil.MockMountInfo(miLine)
	defer restore()

	enforcePath := filepath.Join(dir, "enforce")

	err := os.WriteFile(enforcePath, []byte("garbage"), 0644)
	c.Assert(err, check.IsNil)

	enforcing, err := selinux.IsEnforcing()
	c.Assert(err, check.ErrorMatches, "unknown SELinux status: garbage")
	c.Assert(enforcing, check.Equals, false)
}

func (s *selinuxSuite) TestIsEnforcingFailOther(c *check.C) {
	dir := c.MkDir()
	miLine := fmt.Sprintf("41 19 0:18 / %s rw,relatime shared:20 - selinuxfs selinuxfs rw\n", dir)
	restore := osutil.MockMountInfo(miLine)
	defer restore()

	enforcePath := filepath.Join(dir, "enforce")

	err := os.WriteFile(enforcePath, []byte("not-readable"), 0000)
	c.Assert(err, check.IsNil)

	enforcing, err := selinux.IsEnforcing()
	c.Assert(err, check.ErrorMatches, "open .*: permission denied")
	c.Assert(enforcing, check.Equals, false)
}
