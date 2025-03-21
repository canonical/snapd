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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	main "github.com/snapcore/snapd/cmd/snap-gpio-helper"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/osutil/inotify"
	"github.com/snapcore/snapd/testutil"
	"golang.org/x/sys/unix"
	"gopkg.in/check.v1"
	. "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type snapGpioHelperSuite struct {
	testutil.BaseTest

	rootdir              string
	newDeviceCallback    func(cmd string)
	deleteDeviceCallback func(cmd string)
	mockChipInfos        map[string]main.GPIOChardev
	mockStats            map[string]*unix.Stat_t
	udevadmCmd           *testutil.MockCmd
	// This is needed because calls to c.Error in the inotify goroutine are not registered
	callbackErrors []error
}

var _ = Suite(&snapGpioHelperSuite{})

func (s *snapGpioHelperSuite) SetUpTest(c *C) {
	s.rootdir = c.MkDir()
	dirs.SetRootDir(s.rootdir)
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.mockChipInfos = make(map[string]main.GPIOChardev)
	s.mockStats = make(map[string]*unix.Stat_t)

	// Mock experimental.gpio-chardev-interface
	c.Assert(os.MkdirAll(dirs.FeaturesDir, 0755), check.IsNil)
	c.Assert(os.WriteFile(features.GPIOChardevInterface.ControlFile(), []byte(nil), 0644), check.IsNil)

	// Allow mocking gpio chardev devices
	restore := main.MockGetGpioInfo(func(path string) (main.GPIOChardev, error) {
		chip, ok := s.mockChipInfos[path]
		if !ok {
			return nil, fmt.Errorf("unexpected gpio chip path %s", path)
		}
		return chip, nil
	})
	s.AddCleanup(restore)
	// and their stat
	restore = main.MockUnixStat(func(path string, stat *unix.Stat_t) (err error) {
		target, ok := s.mockStats[path]
		if !ok {
			return fmt.Errorf("unexpected path %s", path)
		}
		*stat = *target
		return nil
	})
	s.AddCleanup(restore)

	// Mock gpio-aggregator sysfs structure
	c.Assert(os.MkdirAll(filepath.Join(s.rootdir, "/sys/bus/platform/drivers/gpio-aggregator"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(s.rootdir, "/sys/bus/platform/drivers/gpio-aggregator/new_device"), nil, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(s.rootdir, "/sys/bus/platform/drivers/gpio-aggregator/delete_device"), nil, 0644), IsNil)
	// Mock gpio-aggregator new_device/delete_device sysfs calls
	s.newDeviceCallback = func(cmd string) {
		mockStat := &unix.Stat_t{
			Rdev: 0x100,
			Mode: unix.S_IFCHR | 0644,
		}
		s.mockChip(c, "gpiochip10", filepath.Join(s.rootdir, "/dev/gpiochip10"), "gpio-aggregator.10", 7, mockStat)
	}
	s.deleteDeviceCallback = func(cmd string) {
		// cmd should be the chip label
		s.removeMockedChipInfo(cmd)
	}
	// Setup watcher
	watcherDone := make(chan struct{})
	s.AddCleanup(func() { close(watcherDone) })
	watcher, err := inotify.NewWatcher()
	c.Assert(err, IsNil)
	c.Assert(watcher.AddWatch(filepath.Join(s.rootdir, "/sys/bus/platform/drivers/gpio-aggregator/new_device"), inotify.InModify), IsNil)
	c.Assert(watcher.AddWatch(filepath.Join(s.rootdir, "/sys/bus/platform/drivers/gpio-aggregator/delete_device"), inotify.InModify), IsNil)
	go func() {
		for {
			select {
			case event := <-watcher.Event:
				path := event.Name
				cmd, err := os.ReadFile(path)
				s.callbackErrors = append(s.callbackErrors, err)
				switch {
				case strings.HasSuffix(path, "new_device"):
					s.newDeviceCallback(string(cmd))
				case strings.HasSuffix(path, "delete_device"):
					s.deleteDeviceCallback(string(cmd))
				default:
					s.callbackErrors = append(s.callbackErrors, fmt.Errorf("unexpected gpio-aggregator sysfs event: %q", path))
				}
			case <-watcherDone:
				return
			}
		}
	}()

	// Mock away any real udev interaction
	s.udevadmCmd = testutil.MockCommand(c, "udevadm", "")
	s.AddCleanup(s.udevadmCmd.Restore)

	s.AddCleanup(main.MockUnixMknod(func(path string, mode uint32, dev int) (err error) { return nil }))
}

func (s *snapGpioHelperSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)

	// This is needed because calls to c.Error in the inotify goroutine (in SetUpTest) are not registered
	for _, err := range s.callbackErrors {
		c.Check(err, IsNil)
	}
}

func (s *snapGpioHelperSuite) mockNewDeviceCallback(f func(cmd string)) (restore func()) {
	return testutil.Mock(&s.newDeviceCallback, f)
}

func (s *snapGpioHelperSuite) mockDeleteDeviceCallback(f func(cmd string)) (restore func()) {
	return testutil.Mock(&s.deleteDeviceCallback, f)
}

type mockChipInfo struct {
	name, path, label string
	lines             uint
}

func (chip *mockChipInfo) Name() string {
	return chip.name
}

func (chip *mockChipInfo) Path() string {
	return chip.path
}

func (chip *mockChipInfo) Label() string {
	return chip.label
}

func (chip *mockChipInfo) NumLines() uint {
	return chip.lines
}

func (s *snapGpioHelperSuite) mockChip(c *C, name, path, label string, lines uint, stat *unix.Stat_t) main.GPIOChardev {
	chip := &mockChipInfo{name, path, label, lines}
	s.mockChipInfos[path] = chip
	if stat != nil {
		s.mockStats[path] = stat
	}
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	c.Assert(os.WriteFile(path, nil, 0644), IsNil)
	return chip
}

func (s *snapGpioHelperSuite) removeMockedChipInfo(label string) {
	for path, chip := range s.mockChipInfos {
		if chip.Label() == label {
			delete(s.mockChipInfos, path)
		}
	}
}

func (s *snapGpioHelperSuite) TestExportUnexportGpioChardevRunthrough(c *C) {
	s.mockChip(c, "gpiochip0", filepath.Join(s.rootdir, "/dev/gpiochip0"), "label-0", 3, nil)

	aggregatedChipPath := filepath.Join(s.rootdir, "/dev/gpiochip10")
	slotDevicePath := filepath.Join(s.rootdir, "/dev/snap/gpio-chardev/gadget-name/slot-name")

	mknodCalled := 0
	restore := main.MockUnixMknod(func(path string, mode uint32, dev int) (err error) {
		mknodCalled++
		c.Check(path, Equals, slotDevicePath)
		s.mockChip(c, "gpiochip10", path, "gpio-aggregator.10", 7, nil)
		return nil
	})
	defer restore()

	deleteDeviceDone := make(chan struct{})
	deleteDeviceCalled := 0
	restore = s.mockDeleteDeviceCallback(func(cmd string) {
		deleteDeviceCalled++
		// Validate aggregator command
		c.Check(cmd, Equals, "gpio-aggregator.10")
		// cmd should be the chip label
		s.removeMockedChipInfo(cmd)
		close(deleteDeviceDone)
	})
	defer restore()

	// 1. Export
	err := main.Run([]string{
		"export-chardev", "label-0", "0,2", "gadget-name", "slot-name",
	})
	c.Assert(err, IsNil)

	// Ephermal udev rule is dropped under /run/udev/rules.d
	udevRulePath := filepath.Join(s.rootdir, "/run/udev/rules.d/69-snap.gadget-name.interface.gpio-chardev-slot-name.rules")
	expectedRule := `SUBSYSTEM=="gpio", KERNEL=="gpiochip10", TAG+="snap_gadget-name_interface_gpio_chardev_slot-name"` + "\n"
	c.Check(udevRulePath, testutil.FileEquals, expectedRule)
	// Udev rules are reloaded and triggered
	c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "control", "--reload-rules"},
		{"udevadm", "trigger", "--name-match", "gpiochip10"},
	})
	// And virtual slot device is created
	c.Check(mknodCalled, Equals, 1)

	// 2. Unexport
	err = main.Run([]string{
		"unexport-chardev", "label-0", "0,2", "gadget-name", "slot-name",
	})
	c.Assert(err, IsNil)

	select {
	case <-deleteDeviceDone:
	case <-time.After(2 * time.Second):
		c.Fatal("sysfs delete_device was not called")
	}

	// Virtual device is removed
	c.Check(slotDevicePath, testutil.FileAbsent)
	// Udev rule is removed
	c.Check(udevRulePath, testutil.FileAbsent)
	// Aggregator device is deleted
	c.Check(s.mockChipInfos[aggregatedChipPath], IsNil)
	c.Check(s.mockChipInfos[slotDevicePath], IsNil)
}

func (s *snapGpioHelperSuite) TestGpioChardevExperimentlFlagUnset(c *C) {
	c.Assert(os.Remove(features.GPIOChardevInterface.ControlFile()), check.IsNil)

	err := main.Run([]string{
		"export-chardev", "label-0", "0,2", "gadget-name", "slot-name",
	})
	c.Check(err, ErrorMatches, `gpio-chardev interface requires the "experimental.gpio-chardev-interface" flag to be set`)

	err = main.Run([]string{
		"unexport-chardev", "label-0", "0,2", "gadget-name", "slot-name",
	})
	c.Check(err, ErrorMatches, `gpio-chardev interface requires the "experimental.gpio-chardev-interface" flag to be set`)
}
