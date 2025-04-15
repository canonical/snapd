// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	main "github.com/snapcore/snapd/cmd/snap-gpio-helper"
	. "gopkg.in/check.v1"
)

func (s *snapGpioHelperSuite) TestUnexportGpioChardev(c *C) {
	called := 0
	restore := main.MockGpioUnxportGadgetChardevChip(func(gadgetName, slotName string) error {
		called++
		c.Check(gadgetName, Equals, "gadget-name")
		c.Check(slotName, Equals, "slot-name")
		return nil
	})
	defer restore()

	err := main.Run([]string{
		"unexport-chardev", "label-0,label-1", "7,0-6,8-100", "gadget-name", "slot-name",
	})
	c.Check(err, IsNil)
	c.Assert(called, Equals, 1)
}
