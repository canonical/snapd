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
	"os"
	"path/filepath"
	"time"

	main "github.com/snapcore/snapd/cmd/snap-gpio-helper"
	"github.com/snapcore/snapd/testutil"
	. "gopkg.in/check.v1"
)

func (s *snapGpioHelperSuite) TestUnexportGpioChardev(c *C) {
	// Mock gadget slot virtual device
	chipPath := filepath.Join(s.rootdir, "/dev/snap/gpio-chardev/gadget-name/slot-name")
	s.mockChip(c, "gpiochip3", chipPath, "gpio-aggregator.0", 7, nil)
	c.Assert(chipPath, testutil.FilePresent)
	// Mock udev rule
	udevRulePath := filepath.Join(s.rootdir, "/run/udev/rules.d/69-snap.gadget-name.interface.gpio-chardev-slot-name.rules")
	c.Assert(os.MkdirAll(filepath.Dir(udevRulePath), 0755), IsNil)
	c.Assert(os.WriteFile(udevRulePath, nil, 0644), IsNil)

	locked, unlocked := 0, 0
	restore := main.MockLockAggregator(func() (unlocker func(), err error) {
		locked++
		return func() {
			unlocked++
		}, nil
	})
	defer restore()

	deleteDeviceDone := make(chan struct{})
	deleteDeviceCalled := 0
	s.mockDeleteDeviceCallback(func(cmd string) {
		deleteDeviceCalled++
		// Validate aggregator command
		c.Check(cmd, Equals, "gpio-aggregator.0")
		// cmd should be the chip label
		s.removeMockedChipInfo(cmd)
		close(deleteDeviceDone)
	})

	err := main.Run([]string{
		"unexport-chardev", "label-2", "0-6", "gadget-name", "slot-name",
	})
	c.Assert(err, IsNil)

	select {
	case <-deleteDeviceDone:
	case <-time.After(2 * time.Second):
		c.Fatal("sysfs delete_device was not called")
	}

	// Aggregator was locked and unlocked
	c.Check(locked, Equals, 1)
	c.Check(unlocked, Equals, 1)
	// Virtual device is removed
	c.Check(chipPath, testutil.FileAbsent)
	// Udev rule is removed
	c.Check(udevRulePath, testutil.FileAbsent)
	// Aggregator device is deleted
	c.Check(deleteDeviceCalled, Equals, 1)
}
