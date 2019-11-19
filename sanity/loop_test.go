/*
 * Copyright (C) 2019 Canonical Ltd
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

package sanity_test

import (
	. "gopkg.in/check.v1"

	"fmt"
	"io/ioutil"
	"path/filepath"

	"github.com/snapcore/snapd/sanity"
)

func (s *sanitySuite) TestCheckLoopControlDeviceHappy(c *C) {
	tmp := c.MkDir()
	path := filepath.Join(tmp, "loop-control")
	c.Assert(ioutil.WriteFile(path, nil, 0644), IsNil)

	restore := sanity.MockLoopControlPath(path)
	defer restore()

	restoreMinorMajor := sanity.MockMajorMinor(func(int) (maj int, min int) {
		return 10, 237
	})
	defer restoreMinorMajor()

	c.Check(sanity.CheckLoopControl(), IsNil)
}

func (s *sanitySuite) TestCheckLoopControlNoDevice(c *C) {
	restore := sanity.MockLoopControlPath("/some/path")
	defer restore()
	c.Check(sanity.CheckLoopControl(), ErrorMatches, `cannot stat "/some/path": no such file or directory`)
}

func (s *sanitySuite) TestCheckLoopControlDeviceWrongMajorMinor(c *C) {
	tmp := c.MkDir()
	path := filepath.Join(tmp, "loop-control")
	c.Assert(ioutil.WriteFile(path, nil, 0644), IsNil)

	restore := sanity.MockLoopControlPath(path)
	defer restore()

	major := 1
	minor := 237
	restoreMinorMajor := sanity.MockMajorMinor(func(int) (maj int, min int) {
		return major, minor
	})
	defer restoreMinorMajor()

	c.Check(sanity.CheckLoopControl(), ErrorMatches, fmt.Sprintf(`unexpected major number for "%s"`, path))

	major = 10
	minor = 1
	c.Check(sanity.CheckLoopControl(), ErrorMatches, fmt.Sprintf(`unexpected minor number for "%s"`, path))
}
