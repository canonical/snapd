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

package device_test

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
)

type gpioChardevTestSuite struct{}

var _ = Suite(&gpioChardevTestSuite{})

func (s *gpioChardevTestSuite) TestSnapGpioChardevPath(c *C) {
	rootdir := c.MkDir()
	dirs.SetRootDir(rootdir)

	devPath := device.SnapGpioChardevPath("snap-name", "slot-name")
	c.Check(devPath, Equals, filepath.Join(rootdir, "/dev/snap/gpio-chardev/snap-name/slot-name"))
}

func (s *gpioChardevTestSuite) TestIoctlGetChipInfo(c *C) {
	tmpdir := c.MkDir()
	chipPath := filepath.Join(tmpdir, "gpiochip0")
	c.Assert(os.WriteFile(chipPath, nil, 0644), IsNil)

	called := 0
	restore := device.MockUnixSyscall(func(trap, a1, a2, a3 uintptr) (uintptr, uintptr, syscall.Errno) {
		called++
		// a3 is ptr to return struct, this cannot be mocked or tested due to needed unsafe pointer operations
		fd, ioctl := a1, a2
		// Validate syscall
		c.Check(trap, Equals, uintptr(unix.SYS_IOCTL))
		// validate path for passed fd
		path, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
		c.Assert(err, IsNil)
		c.Check(path, Equals, chipPath)
		// validate GPIO_GET_CHIPINFO_IOCTL ioctl
		c.Check(ioctl, Equals, uintptr(0x8044b401))
		return 0, 0, 0
	})
	defer restore()

	_, err := device.IoctlGetChipInfo(chipPath)
	c.Assert(err, IsNil)
	c.Assert(called, Equals, 1)
}

func (s *gpioChardevTestSuite) TestGpioChardevInfo(c *C) {
	tmpdir := c.MkDir()
	chipPath := filepath.Join(tmpdir, "gpiochip0")
	c.Assert(os.WriteFile(chipPath, nil, 0644), IsNil)

	called := 0
	restore := device.MockIoctlGetChipInfo(func(path string) (name [32]byte, label [32]byte, lines uint32, err error) {
		called++
		c.Assert(path, Equals, chipPath)
		copy(name[:], "gpiochip0\x00")
		copy(label[:], "label-0\x00")
		return name, label, 12, nil
	})
	defer restore()

	chip, err := device.GetGpioChardevChipInfo(chipPath)
	c.Assert(err, IsNil)
	c.Check(chip.Path, Equals, chipPath)
	c.Check(chip.Name, Equals, "gpiochip0")
	c.Check(chip.Label, Equals, "label-0")
	c.Check(chip.NumLines, Equals, uint(12))
	c.Check(fmt.Sprintf("%s", chip), Equals, "(name: gpiochip0, label: label-0, lines: 12)")

	c.Assert(called, Equals, 1)
}
