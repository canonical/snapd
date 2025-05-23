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

package gpio_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/inotify"
	"github.com/snapcore/snapd/sandbox/gpio"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type chardevTestSuite struct{}

var _ = Suite(&chardevTestSuite{})

func (s *chardevTestSuite) TestSnapChardevPath(c *C) {
	rootdir := c.MkDir()
	dirs.SetRootDir(rootdir)
	defer dirs.SetRootDir("")

	devPath := gpio.SnapChardevPath("snap-name", "slot-name")
	c.Check(devPath, Equals, filepath.Join(rootdir, "/dev/snap/gpio-chardev/snap-name/slot-name"))
}

func (s *chardevTestSuite) TestIoctlGetChipInfo(c *C) {
	tmpdir := c.MkDir()
	chipPath := filepath.Join(tmpdir, "gpiochip0")
	c.Assert(os.WriteFile(chipPath, nil, 0644), IsNil)

	called := 0
	restore := gpio.MockUnixSyscall(func(trap, a1, a2, a3 uintptr) (uintptr, uintptr, syscall.Errno) {
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

	_, err := gpio.IoctlGetChipInfo(chipPath)
	c.Assert(err, IsNil)
	c.Assert(called, Equals, 1)
}

func (s *chardevTestSuite) TestChardevChipInfo(c *C) {
	tmpdir := c.MkDir()
	chipPath := filepath.Join(tmpdir, "gpiochip0")
	c.Assert(os.WriteFile(chipPath, nil, 0644), IsNil)

	called := 0
	restore := gpio.MockIoctlGetChipInfo(func(path string) (name [32]byte, label [32]byte, lines uint32, err error) {
		called++
		c.Assert(path, Equals, chipPath)
		copy(name[:], "gpiochip0\x00")
		copy(label[:], "label-0\x00")
		return name, label, 12, nil
	})
	defer restore()

	chip, err := gpio.ChardevChipInfo(chipPath)
	c.Assert(err, IsNil)
	c.Check(chip.Path, Equals, chipPath)
	c.Check(chip.Name, Equals, "gpiochip0")
	c.Check(chip.Label, Equals, "label-0")
	c.Check(chip.NumLines, Equals, uint(12))
	c.Check(fmt.Sprintf("%s", chip), Equals, "(name: gpiochip0, label: label-0, lines: 12)")

	c.Assert(called, Equals, 1)
}

func (s *chardevTestSuite) TestChardevChipInfoNoChipError(c *C) {
	mockPath := "/path/to/chip"

	called := 0
	restore := gpio.MockIoctlGetChipInfo(func(path string) (name [32]byte, label [32]byte, lines uint32, err error) {
		called++
		c.Assert(path, Equals, mockPath)
		err = errors.New("boom!")
		return
	})
	defer restore()

	_, err := gpio.ChardevChipInfo(mockPath)
	c.Assert(err, ErrorMatches, `cannot read gpio chip info from "/path/to/chip": boom!`)
	c.Assert(called, Equals, 1)
}

func (s *chardevTestSuite) TestEnsureAggregatorDriver(c *C) {
	rootdir := c.MkDir()
	dirs.SetRootDir(rootdir)
	defer dirs.SetRootDir("")

	called := 0
	restore := gpio.MockKmodLoadModule(func(module string, options []string) error {
		called++
		return nil
	})
	defer restore()

	// 1. gpio-aggregator module is already loaded
	c.Assert(os.MkdirAll(filepath.Join(rootdir, "/sys/bus/platform/drivers/gpio-aggregator"), 0755), IsNil)
	// But snapd (and kernel) support is not there yet
	c.Check(gpio.EnsureAggregatorDriver(), ErrorMatches, "gpio-aggregator configfs support is missing")
	// Loading the module is not attempted
	c.Check(called, Equals, 0)

	// 2. gpio-aggregator module is missing
	c.Assert(os.RemoveAll(filepath.Join(rootdir, "/sys/bus/platform/drivers/gpio-aggregator")), IsNil)
	// But snapd (and kernel) support is not there yet
	c.Check(gpio.EnsureAggregatorDriver(), ErrorMatches, "gpio-aggregator configfs support is missing")
	// Loading the module is attempted
	c.Check(called, Equals, 1)
}

type exportUnexportTestSuite struct {
	testutil.BaseTest

	rootdir              string
	newDeviceCallback    func(cmd string)
	deleteDeviceCallback func(cmd string)
	mockChipInfos        map[string]*gpio.ChardevChip
	mockStats            map[string]fs.FileInfo
	udevadmCmd           *testutil.MockCmd
	// This is needed because calls to c.Error in the inotify goroutine are not registered
	callbackErrors []error

	mu sync.Mutex
}

var _ = Suite(&exportUnexportTestSuite{})

const mockMajor, mockMinor = 254, 10

func (s *exportUnexportTestSuite) SetUpTest(c *C) {
	s.rootdir = c.MkDir()
	dirs.SetRootDir(s.rootdir)
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.mockChipInfos = make(map[string]*gpio.ChardevChip)
	s.mockStats = make(map[string]fs.FileInfo)

	// Allow mocking gpio chardev devices
	restore := gpio.MockChardevChipInfo(func(path string) (*gpio.ChardevChip, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		chip, ok := s.mockChipInfos[path]
		if !ok {
			return nil, fmt.Errorf("unexpected gpio chip path %s", path)
		}
		return chip, nil
	})
	s.AddCleanup(restore)
	// and their stat
	restore = gpio.MockOsStat(func(path string) (fs.FileInfo, error) {
		target, ok := s.mockStats[path]
		if !ok {
			return nil, fmt.Errorf("unexpected path %s", path)
		}
		return target, nil
	})
	s.AddCleanup(restore)

	// Mock default gpio chardev device (254:10) driver symlinks
	// The driver is used for detecting if the matched device is an
	// already aggregated device
	c.Assert(os.MkdirAll(filepath.Join(s.rootdir, "/sys/dev/char"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(s.rootdir, "/sys/devices/platform/mock-device/gpiochip10"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(s.rootdir, "/sys/bus/platform/drivers/mock-driver"), 0755), IsNil)
	c.Assert(os.Symlink("../../devices/platform/mock-device/gpiochip10", filepath.Join(s.rootdir, "/sys/dev/char/254:10")), IsNil)
	c.Assert(os.Symlink("../../../bus/platform/drivers/mock-driver", filepath.Join(s.rootdir, "/sys/devices/platform/mock-device/driver")), IsNil)

	// Mock gpio-aggregator sysfs structure
	c.Assert(os.MkdirAll(filepath.Join(s.rootdir, "/sys/bus/platform/drivers/gpio-aggregator"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(s.rootdir, "/sys/bus/platform/drivers/gpio-aggregator/new_device"), nil, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(s.rootdir, "/sys/bus/platform/drivers/gpio-aggregator/delete_device"), nil, 0644), IsNil)
	// Mock gpio-aggregator new_device/delete_device sysfs calls
	s.newDeviceCallback = func(cmd string) {
		mockStat := &syscall.Stat_t{
			Rdev: unix.Mkdev(mockMajor, mockMinor),
			Mode: syscall.S_IFCHR | 0644,
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
	c.Assert(watcher.AddWatch(filepath.Join(s.rootdir, "/sys/bus/platform/drivers/gpio-aggregator/new_device"), inotify.InCloseWrite), IsNil)
	c.Assert(watcher.AddWatch(filepath.Join(s.rootdir, "/sys/bus/platform/drivers/gpio-aggregator/delete_device"), inotify.InCloseWrite), IsNil)
	go func() {
		for {
			select {
			case event := <-watcher.Event:
				s.mu.Lock()
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
				s.mu.Unlock()
			case <-watcherDone:
				return
			}
		}
	}()

	// Mock away any real udev interaction
	s.udevadmCmd = testutil.MockCommand(c, "udevadm", "")
	s.AddCleanup(s.udevadmCmd.Restore)

	s.AddCleanup(gpio.MockSyscallMknod(func(path string, mode uint32, dev int) (err error) { return nil }))
	s.AddCleanup(gpio.MockOsChmod(func(name string, mode fs.FileMode) error { return nil }))
	s.AddCleanup(gpio.MockOsChown(func(name string, uid, gid int) error { return nil }))
}

func (s *exportUnexportTestSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)

	s.mu.Lock()
	defer s.mu.Unlock()
	// This is needed because calls to c.Error in the inotify goroutine (in SetUpTest) are not registered
	for _, err := range s.callbackErrors {
		c.Check(err, IsNil)
	}
}

func (s *exportUnexportTestSuite) mockNewDeviceCallback(f func(cmd string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.newDeviceCallback = f
}

func (s *exportUnexportTestSuite) mockDeleteDeviceCallback(f func(cmd string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteDeviceCallback = f
}

type fakeFileInfo struct {
	os.FileInfo

	stat *syscall.Stat_t
}

func (info *fakeFileInfo) Sys() any {
	return info.stat
}

func (s *exportUnexportTestSuite) mockChip(c *C, name, path, label string, lines uint, stat *syscall.Stat_t) *gpio.ChardevChip {
	chip := &gpio.ChardevChip{
		Path:     path,
		Name:     name,
		Label:    label,
		NumLines: lines,
	}
	s.mockChipInfos[path] = chip
	if stat == nil {
		stat = &syscall.Stat_t{
			Rdev: unix.Mkdev(mockMajor, mockMinor),
			Mode: syscall.S_IFCHR | 0600,
			Uid:  1003,
			Gid:  1004,
		}
	}
	fmode := fs.FileMode(stat.Mode & 0777)
	if stat.Mode&syscall.S_IFMT == syscall.S_IFCHR {
		fmode = fmode | fs.ModeDevice | fs.ModeCharDevice
	}
	s.mockStats[path] = &fakeFileInfo{
		testutil.FakeFileInfo(path, fmode),
		stat,
	}
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	c.Assert(os.WriteFile(path, nil, 0644), IsNil)
	return chip
}

func (s *exportUnexportTestSuite) removeMockedChipInfo(label string) {
	for path, chip := range s.mockChipInfos {
		if chip.Label == label {
			delete(s.mockChipInfos, path)
		}
	}
}

func (s *exportUnexportTestSuite) TestExportGadgetChardevChip(c *C) {
	s.mockChip(c, "gpiochip0", filepath.Join(s.rootdir, "/dev/gpiochip0"), "label-0", 3, nil)
	s.mockChip(c, "gpiochip1", filepath.Join(s.rootdir, "/dev/gpiochip1"), "label-1", 6, nil)
	s.mockChip(c, "gpiochip2", filepath.Join(s.rootdir, "/dev/gpiochip2"), "label-2", 9, nil)

	aggregatorLock, err := osutil.OpenExistingLockForReading(filepath.Join(s.rootdir, "/sys/bus/platform/drivers/gpio-aggregator"))
	c.Assert(err, IsNil)

	mockStat := &syscall.Stat_t{
		Rdev: unix.Mkdev(254, 3),
		Mode: syscall.S_IFCHR | 0600,
		Uid:  1001,
		Gid:  1002,
	}
	s.mockNewDeviceCallback(func(cmd string) {
		// Creating a new aggregator device is synchronized with a lock.
		c.Check(aggregatorLock.TryLock(), Equals, osutil.ErrAlreadyLocked)
		// Validate aggregator command
		c.Check(cmd, Equals, "label-2 0-6")
		// Mock aggregated chip creation
		chipPath := filepath.Join(s.rootdir, "/dev/gpiochip3")
		s.mockChip(c, "gpiochip3", chipPath, "gpio-aggregator.0", 7, mockStat)
	})

	mknodCalled := 0
	restore := gpio.MockSyscallMknod(func(path string, mode uint32, dev int) (err error) {
		mknodCalled++
		c.Check(path, Equals, filepath.Join(s.rootdir, "/dev/snap/gpio-chardev/gadget-name/slot-name"))
		// permission bits are masked to zero
		c.Check(mode, Equals, uint32(unix.S_IFCHR))
		c.Check(unix.Major(uint64(dev)), Equals, uint32(254))
		c.Check(unix.Minor(uint64(dev)), Equals, uint32(3))
		s.mockChip(c, "gpiochip3", path, "gpio-aggregator.0", 7, mockStat)
		return nil
	})
	defer restore()

	chmodCalled := 0
	restore = gpio.MockOsChmod(func(path string, mode fs.FileMode) error {
		chmodCalled++
		c.Check(path, Equals, filepath.Join(s.rootdir, "/dev/snap/gpio-chardev/gadget-name/slot-name"))
		c.Check(mode, Equals, fs.FileMode(0600)|fs.ModeDevice|fs.ModeCharDevice)
		return nil
	})
	defer restore()

	chownCalled := 0
	restore = gpio.MockOsChown(func(path string, uid, gid int) error {
		chownCalled++
		c.Check(path, Equals, filepath.Join(s.rootdir, "/dev/snap/gpio-chardev/gadget-name/slot-name"))
		c.Check(uid, Equals, 1001)
		c.Check(gid, Equals, 1002)
		return nil
	})
	defer restore()

	err = gpio.ExportGadgetChardevChip(context.TODO(), []string{"label-2"}, strutil.Range{{Start: 0, End: 6}}, "gadget-name", "slot-name")
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
	// And original permission bits and ownership are replicated
	c.Check(chmodCalled, Equals, 1)
	c.Check(chownCalled, Equals, 1)
}

func (s *exportUnexportTestSuite) TestExportGadgetChardevChipMissingLine(c *C) {
	s.mockChip(c, "gpiochip0", filepath.Join(s.rootdir, "/dev/gpiochip0"), "label-0", 3, nil)

	err := gpio.ExportGadgetChardevChip(context.TODO(), []string{"label-0"}, strutil.Range{{Start: 0, End: 3}}, "gadget-name", "slot-name")
	c.Check(err, ErrorMatches, `invalid lines argument: invalid line offset 3: line does not exist in "gpiochip0"`)
}

func (s *exportUnexportTestSuite) TestExportGadgetChardevChipMissingChip(c *C) {
	err := gpio.ExportGadgetChardevChip(context.TODO(), []string{"label-0"}, strutil.Range{{Start: 0, End: 0}}, "gadget-name", "slot-name")
	c.Check(err, ErrorMatches, "no matching gpio chips found matching chip labels")
}

func (s *exportUnexportTestSuite) TestExportGadgetChardevChipMultipleMatchingChips(c *C) {
	s.mockChip(c, "gpiochip0", filepath.Join(s.rootdir, "/dev/gpiochip0"), "label-0", 3, nil)
	s.mockChip(c, "gpiochip1", filepath.Join(s.rootdir, "/dev/gpiochip1"), "label-1", 6, nil)

	err := gpio.ExportGadgetChardevChip(context.TODO(), []string{"label-0", "label-1"}, strutil.Range{{Start: 0, End: 0}}, "gadget-name", "slot-name")
	c.Check(err, ErrorMatches, `more than one gpio chips were found matching chip labels \(label-0 label-1\)`)
}

func (s *exportUnexportTestSuite) TestExportGadgetChardevChipTimeout(c *C) {
	s.mockChip(c, "gpiochip0", filepath.Join(s.rootdir, "/dev/gpiochip0"), "label-0", 3, nil)

	// Do nothing to force waiting
	s.mockNewDeviceCallback(func(cmd string) {})

	restore := gpio.MockAggregatorCreationTimeout(100 * time.Millisecond)
	defer restore()

	err := gpio.ExportGadgetChardevChip(context.TODO(), []string{"label-0"}, strutil.Range{{Start: 0, End: 0}}, "gadget-name", "slot-name")
	c.Check(err, ErrorMatches, "timeout waiting for aggregator device to appear")
}

func (s *exportUnexportTestSuite) TestExportGadgetChardevChipContextCanellation(c *C) {
	s.mockChip(c, "gpiochip0", filepath.Join(s.rootdir, "/dev/gpiochip0"), "label-0", 3, nil)

	ctx, cancel := context.WithCancel(context.TODO())
	s.mockNewDeviceCallback(func(cmd string) {
		cancel()
	})

	err := gpio.ExportGadgetChardevChip(ctx, []string{"label-0"}, strutil.Range{{Start: 0, End: 0}}, "gadget-name", "slot-name")
	c.Check(err, ErrorMatches, "context canceled")
}

func (s *exportUnexportTestSuite) TestExportGadgetChardevChipUdevReloadError(c *C) {
	s.mockChip(c, "gpiochip0", filepath.Join(s.rootdir, "/dev/gpiochip0"), "label-0", 3, nil)

	cmd := testutil.MockCommand(c, "udevadm", "echo boom! && exit 1")
	defer cmd.Restore()

	err := gpio.ExportGadgetChardevChip(context.TODO(), []string{"label-0"}, strutil.Range{{Start: 0, End: 0}}, "gadget-name", "slot-name")
	c.Check(err, ErrorMatches, "cannot reload udev rules: boom!")
}

func (s *exportUnexportTestSuite) TestExportGadgetChardevChipAddGadgetDeviceError(c *C) {
	s.mockChip(c, "gpiochip0", filepath.Join(s.rootdir, "/dev/gpiochip0"), "label-0", 3, nil)

	restore := gpio.MockSyscallMknod(func(path string, mode uint32, dev int) (err error) {
		c.Assert(path, Equals, filepath.Join(s.rootdir, "/dev/snap/gpio-chardev/gadget-name/slot-name"))
		c.Assert(os.WriteFile(path, nil, 0644), IsNil)
		return nil
	})
	defer restore()

	restore = gpio.MockOsChown(func(path string, uid, gid int) error {
		return errors.New("boom!")
	})
	defer restore()

	err := gpio.ExportGadgetChardevChip(context.TODO(), []string{"label-0"}, strutil.Range{{Start: 0, End: 0}}, "gadget-name", "slot-name")
	c.Check(err, ErrorMatches, "boom!")
	// device node is cleaned on error
	c.Check(filepath.Join(s.rootdir, "/dev/snap/gpio-chardev/gadget-name/slot-name"), testutil.FileAbsent)
}

func (s *exportUnexportTestSuite) TestExportGadgetChardevChipAggregatedChipsSkipped(c *C) {
	mockStat := &syscall.Stat_t{
		Rdev: unix.Mkdev(254, 1),
		Mode: syscall.S_IFCHR | 0600,
		Uid:  1001,
		Gid:  1002,
	}
	s.mockChip(c, "gpiochip1", filepath.Join(s.rootdir, "/dev/gpiochip1"), "label-1", 3, mockStat)

	// Mock gpio chardev device (254:1) with gpio-aggregator driver symlinks
	c.Assert(os.MkdirAll(filepath.Join(s.rootdir, "/sys/dev/char"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(s.rootdir, "/sys/devices/platform/mock-aggregator-device/gpiochip1"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(s.rootdir, "/sys/bus/platform/drivers/gpio-aggregator"), 0755), IsNil)
	c.Assert(os.Symlink("../../devices/platform/mock-aggregator-device/gpiochip1", filepath.Join(s.rootdir, "/sys/dev/char/254:1")), IsNil)
	c.Assert(os.Symlink("../../../bus/platform/drivers/gpio-aggregator", filepath.Join(s.rootdir, "/sys/devices/platform/mock-aggregator-device/driver")), IsNil)

	err := gpio.ExportGadgetChardevChip(context.TODO(), []string{"label-1"}, strutil.Range{{Start: 0, End: 0}}, "gadget-name", "slot-name")
	c.Check(err, ErrorMatches, "no matching gpio chips found matching chip labels")
}

func (s *exportUnexportTestSuite) TestUnexportGpioChardev(c *C) {
	// Mock gadget slot virtual device
	chipPath := filepath.Join(s.rootdir, "/dev/snap/gpio-chardev/gadget-name/slot-name")
	s.mockChip(c, "gpiochip3", chipPath, "gpio-aggregator.0", 7, nil)
	c.Assert(chipPath, testutil.FilePresent)
	// Mock udev rule
	udevRulePath := filepath.Join(s.rootdir, "/run/udev/rules.d/69-snap.gadget-name.interface.gpio-chardev-slot-name.rules")
	c.Assert(os.MkdirAll(filepath.Dir(udevRulePath), 0755), IsNil)
	c.Assert(os.WriteFile(udevRulePath, nil, 0644), IsNil)

	locked, unlocked := 0, 0
	restore := gpio.MockLockAggregator(func() (unlocker func(), err error) {
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

	err := gpio.UnexportGadgetChardevChip("gadget-name", "slot-name")
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

func (s *exportUnexportTestSuite) TestExportUnexportGpioChardevRunthrough(c *C) {
	s.mockChip(c, "gpiochip0", filepath.Join(s.rootdir, "/dev/gpiochip0"), "label-0", 3, nil)

	aggregatedChipPath := filepath.Join(s.rootdir, "/dev/gpiochip10")
	slotDevicePath := filepath.Join(s.rootdir, "/dev/snap/gpio-chardev/gadget-name/slot-name")

	mknodCalled := 0
	restore := gpio.MockSyscallMknod(func(path string, mode uint32, dev int) (err error) {
		mknodCalled++
		c.Check(path, Equals, slotDevicePath)
		s.mockChip(c, "gpiochip10", path, "gpio-aggregator.10", 7, nil)
		return nil
	})
	defer restore()

	deleteDeviceDone := make(chan struct{})
	deleteDeviceCalled := 0
	s.mockDeleteDeviceCallback(func(cmd string) {
		deleteDeviceCalled++
		// Validate aggregator command
		c.Check(cmd, Equals, "gpio-aggregator.10")
		// cmd should be the chip label
		s.removeMockedChipInfo(cmd)
		close(deleteDeviceDone)
	})

	// 1. Export
	err := gpio.ExportGadgetChardevChip(context.TODO(), []string{"label-0"}, strutil.Range{{Start: 0, End: 2}}, "gadget-name", "slot-name")
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
	err = gpio.UnexportGadgetChardevChip("gadget-name", "slot-name")
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
