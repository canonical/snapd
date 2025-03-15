// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package gadget_test

import (
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
)

type gpioChardevTestSuite struct{}

var _ = Suite(&gpioChardevTestSuite{})

func (s *gpioChardevTestSuite) TestSnapGpioChardevPath(c *C) {
	rootdir := c.MkDir()
	dirs.SetRootDir(rootdir)

	devPath := gadget.SnapGpioChardevPath("snap-name", "slot-name")
	c.Check(devPath, Equals, filepath.Join(rootdir, "/dev/snap/gpio-chardev/snap-name/slot-name"))
}
