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
	"errors"
	"path/filepath"
	"time"

	main "github.com/snapcore/snapd/cmd/snap-gpio-helper"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
	"golang.org/x/sys/unix"
	. "gopkg.in/check.v1"
)

func (s *snapGpioHelperSuite) TestExportGpioChardev(c *C) {
	s.mockChip(c, "gpiochip0", filepath.Join(s.rootdir, "/dev/gpiochip0"), "label-0", 3, nil)
	s.mockChip(c, "gpiochip1", filepath.Join(s.rootdir, "/dev/gpiochip1"), "label-1", 6, nil)
	s.mockChip(c, "gpiochip2", filepath.Join(s.rootdir, "/dev/gpiochip2"), "label-2", 9, nil)

	aggregatorLock, err := osutil.OpenExistingLockForReading(filepath.Join(s.rootdir, "/sys/bus/platform/drivers/gpio-aggregator"))
	c.Assert(err, IsNil)

	restore := s.mockNewDeviceCallback(func(cmd string) {
		// Creating a new aggregator device is synchronized with a lock.
		c.Check(aggregatorLock.TryLock(), Equals, osutil.ErrAlreadyLocked)
		// Validate aggregator command
		c.Check(cmd, Equals, "label-2 0-6")
		// Mock aggregated chip creation
		chipPath := filepath.Join(s.rootdir, "/dev/gpiochip3")
		mockStat := &unix.Stat_t{
			Rdev: 0x101,
			Mode: unix.S_IFCHR | 0600,
		}
		s.mockChip(c, "gpiochip3", chipPath, "gpio-aggregator.0", 7, mockStat)
	})
	defer restore()

	mknodCalled := 0
	restore = main.MockUnixMknod(func(path string, mode uint32, dev int) (err error) {
		mknodCalled++
		c.Check(path, Equals, filepath.Join(s.rootdir, "/dev/snap/gpio-chardev/gadget-name/slot-name"))
		c.Check(mode, Equals, uint32(unix.S_IFCHR|0600))
		c.Check(dev, Equals, 0x101)
		mockStat := &unix.Stat_t{
			Rdev: 0x101,
			Mode: unix.S_IFCHR | 0600,
		}
		s.mockChip(c, "gpiochip3", path, "gpio-aggregator.0", 7, mockStat)
		return nil
	})
	defer restore()

	err = main.Run([]string{
		"export-chardev", "label-2", "0-6", "gadget-name", "slot-name",
	})
	c.Assert(err, IsNil)

	// Aggregator lock is unlocked
	c.Check(aggregatorLock.TryLock(), IsNil)
	// Unlock for unxport-chardev command below
	aggregatorLock.Unlock()
	// Ephermal udev rule is dropped under /run/udev/rules.d
	udevRulePath := filepath.Join(s.rootdir, "/run/udev/rules.d/69-snap.gadget-name.interface.gpio-chardev-slot-name.rules")
	expectedRule := `SUBSYSTEM=="gpio", KERNEL=="gpiochip3", TAG+="snap_gadget-name_interface_gpio_chardev_slot-name"` + "\n"
	c.Check(udevRulePath, testutil.FileEquals, expectedRule)
	// Udev rules are reloaded and triggered
	c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "control", "--reload-rules"},
		{"udevadm", "trigger", "--name-match", "gpiochip3"},
	})
	// And virtual slot device is created
	c.Check(mknodCalled, Equals, 1)
}

func (s *snapGpioHelperSuite) TestExportGpioChardevBadLine(c *C) {
	s.mockChip(c, "gpiochip0", filepath.Join(s.rootdir, "/dev/gpiochip0"), "label-0", 3, nil)

	for lines, expectedErr := range map[string]string{
		"0-3":   `invalid lines argument: invalid line offset 3: line does not exist in "gpiochip0"`,
		"0-2,1": `invalid lines argument: overlapping range span found "1"`,
		"1-0":   `invalid lines argument: invalid range "1-0": range end has to be larger than range start`,
		"0-":    `invalid lines argument: .*: invalid syntax`,
		"a":     `invalid lines argument: .*: invalid syntax`,
	} {
		err := main.Run([]string{
			"export-chardev", "label-0", lines, "gadget-name", "slot-name",
		})
		c.Check(err, ErrorMatches, expectedErr)
	}
}

func (s *snapGpioHelperSuite) TestExportGpioChardevMissingChip(c *C) {
	err := main.Run([]string{
		"export-chardev", "label-0", "0", "gadget-name", "slot-name",
	})
	c.Check(err, ErrorMatches, "no matching gpio chips found matching passed labels")
}

func (s *snapGpioHelperSuite) TestExportGpioChardevMultipleMatchingChips(c *C) {
	s.mockChip(c, "gpiochip0", filepath.Join(s.rootdir, "/dev/gpiochip0"), "label-0", 3, nil)
	s.mockChip(c, "gpiochip1", filepath.Join(s.rootdir, "/dev/gpiochip1"), "label-1", 6, nil)

	err := main.Run([]string{
		"export-chardev", "label-0,label-1", "0", "gadget-name", "slot-name",
	})
	c.Check(err, ErrorMatches, "more than one gpio chips were found matching passed labels")
}

func (s *snapGpioHelperSuite) TestExportGpioChardevTimeout(c *C) {
	s.mockChip(c, "gpiochip0", filepath.Join(s.rootdir, "/dev/gpiochip0"), "label-0", 3, nil)

	// Do nothing to force waiting
	restore := s.mockNewDeviceCallback(func(cmd string) {})
	defer restore()

	restore = main.MockAggregatorCreationTimeout(1 * time.Nanosecond)
	defer restore()

	err := main.Run([]string{
		"export-chardev", "label-0", "0", "gadget-name", "slot-name",
	})
	c.Check(err, ErrorMatches, "cannot add aggregator device: max timeout exceeded")
}

func (s *snapGpioHelperSuite) TestExportGpioChardevUdevReloadError(c *C) {
	s.mockChip(c, "gpiochip0", filepath.Join(s.rootdir, "/dev/gpiochip0"), "label-0", 3, nil)

	cmd := testutil.MockCommand(c, "udevadm", "echo boom! && exit 1")
	defer cmd.Restore()

	err := main.Run([]string{
		"export-chardev", "label-0", "0", "gadget-name", "slot-name",
	})
	c.Check(err, ErrorMatches, "cannot add udev tagging rule: cannot reload udev rules: exit status 1\nudev output:\nboom!\n")
}

func (s *snapGpioHelperSuite) TestExportGpioChardevAddGadgetDeviceError(c *C) {
	s.mockChip(c, "gpiochip0", filepath.Join(s.rootdir, "/dev/gpiochip0"), "label-0", 3, nil)

	restore := main.MockUnixMknod(func(path string, mode uint32, dev int) (err error) {
		return errors.New("boom!")
	})
	defer restore()

	err := main.Run([]string{
		"export-chardev", "label-0", "0", "gadget-name", "slot-name",
	})
	c.Check(err, ErrorMatches, "cannot add gadget slot device: boom!")
}
