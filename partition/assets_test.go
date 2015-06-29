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

package partition

import (
	. "gopkg.in/check.v1"
)

func (s *PartitionTestSuite) TestHardwareSpec(c *C) {

	hardwareSpecFile = makeHardwareYaml(c, "")
	hw, err := readHardwareSpec()
	c.Assert(err, IsNil)
	c.Assert(hw.Kernel, Equals, "assets/vmlinuz")
	c.Assert(hw.Initrd, Equals, "assets/initrd.img")
	c.Assert(hw.DtbDir, Equals, "assets/dtbs")
	c.Assert(hw.PartitionLayout, Equals, bootloaderSystemAB)
	c.Assert(hw.Bootloader, Equals, bootloaderNameUboot)
}
