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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
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

func (s *chardevTestSuite) TestGetChardevChipInfo(c *C) {
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

	chip, err := gpio.GetChardevChipInfo(chipPath)
	c.Assert(err, IsNil)
	c.Check(chip.Path(), Equals, chipPath)
	c.Check(chip.Name(), Equals, "gpiochip0")
	c.Check(chip.Label(), Equals, "label-0")
	c.Check(chip.NumLines(), Equals, uint(12))
	c.Check(fmt.Sprintf("%s", chip), Equals, "(name: gpiochip0, label: label-0, lines: 12)")

	c.Assert(called, Equals, 1)
}

func (s *chardevTestSuite) TestGetChardevChipInfoNoChipError(c *C) {
	mockPath := "/path/to/chip"

	called := 0
	restore := gpio.MockIoctlGetChipInfo(func(path string) (name [32]byte, label [32]byte, lines uint32, err error) {
		called++
		c.Assert(path, Equals, mockPath)
		err = errors.New("boom!")
		return
	})
	defer restore()

	_, err := gpio.GetChardevChipInfo(mockPath)
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
	// and configfs kernel interface is not supported
	c.Check(gpio.EnsureAggregatorDriver(), ErrorMatches, "gpio-aggregator configfs support is missing: stat .*sys/kernel/config/gpio-aggregator: no such file or directory")
	// Loading the module is not attempted
	c.Check(called, Equals, 0)

	// 2. gpio-aggregator module is not loaded
	c.Assert(os.RemoveAll(filepath.Join(rootdir, "/sys/bus/platform/drivers/gpio-aggregator")), IsNil)
	// and configfs kernel interface is supported
	c.Assert(os.MkdirAll(filepath.Join(rootdir, "/sys/kernel/config/gpio-aggregator"), 0755), IsNil)
	c.Check(gpio.EnsureAggregatorDriver(), IsNil)
	// Loading the module is attempted
	c.Check(called, Equals, 1)

	// 3. gpio-aggregator module loading error
	restore = gpio.MockKmodLoadModule(func(module string, options []string) error { return errors.New("boom!") })
	defer restore()
	c.Check(gpio.EnsureAggregatorDriver(), ErrorMatches, "cannot load gpio-aggregator module: boom!")
}

type exportUnexportTestSuite struct {
	testutil.BaseTest

	rootdir       string
	mockChipInfos map[string]*gpio.ChardevChip
	mockStats     map[string]fs.FileInfo
	udevadmCmd    *testutil.MockCmd
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
	restore := gpio.MockGetChardevChipInfo(func(path string) (*gpio.ChardevChip, error) {
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
	// also mock gpio-aggregator creation
	restore = gpio.MockOsMkdir(func(path string, perm fs.FileMode) error {
		c.Check(path, Equals, filepath.Join(s.rootdir, "/sys/kernel/config/gpio-aggregator/snap.gadget-name.slot-name"))
		c.Check(perm, Equals, fs.FileMode(0755))

		c.Assert(os.MkdirAll(path, perm), IsNil)
		// populate dev_name file that points to the corresponding sysfs directory
		c.Assert(os.WriteFile(filepath.Join(path, "dev_name"), []byte("gpio-aggregator.0\n"), 0644), IsNil)

		// populate corresponding sysfs directory
		c.Assert(os.MkdirAll(filepath.Join(s.rootdir, "/sys/devices/platform/gpio-aggregator.0/gpiochip3"), 0755), IsNil)
		chipPath := filepath.Join(s.rootdir, "/dev/gpiochip3")
		s.mockChip(c, "gpiochip3", chipPath, "gpio-aggregator.0", 7, &syscall.Stat_t{
			Rdev: unix.Mkdev(254, 3),
			Mode: syscall.S_IFCHR | 0600,
			Uid:  1001,
			Gid:  1002,
		})
		return nil
	})
	s.AddCleanup(restore)
	// and deletion
	restore = gpio.MockOsWriteFile(func(path string, data []byte, perm fs.FileMode) error {
		c.Check(path, Equals, filepath.Join(s.rootdir, "/sys/kernel/config/gpio-aggregator/snap.gadget-name.slot-name/live"))
		c.Check(string(data), DeepEquals, "0")
		c.Check(perm, Equals, fs.FileMode(0644))

		// remove configfs files that could have been added during
		// creation because they will block directory removal
		base := filepath.Dir(path)
		entries, err := os.ReadDir(base)
		c.Assert(err, IsNil)
		for _, entry := range entries {
			switch {
			case strings.HasPrefix(entry.Name(), "line"):
				c.Assert(os.Remove(filepath.Join(base, entry.Name(), "key")), IsNil)
				c.Assert(os.Remove(filepath.Join(base, entry.Name(), "offset")), IsNil)
			case entry.Name() == "dev_name":
				c.Assert(os.Remove(filepath.Join(base, entry.Name())), IsNil)
			case entry.Name() == "live":
				c.Assert(os.Remove(filepath.Join(base, entry.Name())), IsNil)
			}
		}

		// remove corresponding sysfs directory
		c.Assert(os.RemoveAll(filepath.Join(s.rootdir, "/sys/devices/platform/gpio-aggregator.0")), IsNil)
		s.removeMockedChipInfo("gpio-aggregator.0")
		return nil
	})
	s.AddCleanup(restore)

	// Mock default gpio chardev device (254:10) driver symlinks
	// The driver is used for detecting if the matched device is an
	// already aggregated device
	c.Assert(os.MkdirAll(filepath.Join(s.rootdir, "/sys/dev/char"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(s.rootdir, "/sys/devices/platform/mock-device/gpiochip0"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(s.rootdir, "/sys/bus/platform/drivers/mock-driver"), 0755), IsNil)
	c.Assert(os.Symlink("../../devices/platform/mock-device/gpiochip0", filepath.Join(s.rootdir, "/sys/dev/char/254:10")), IsNil)
	c.Assert(os.Symlink("../../../bus/platform/drivers/mock-driver", filepath.Join(s.rootdir, "/sys/devices/platform/mock-device/driver")), IsNil)

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

func (s *exportUnexportTestSuite) removeMockedChipInfo(label string) {
	for path, chip := range s.mockChipInfos {
		if chip.Label() == label {
			delete(s.mockChipInfos, path)
		}
	}
}

type fakeFileInfo struct {
	os.FileInfo

	stat *syscall.Stat_t
}

func (info *fakeFileInfo) Sys() any {
	return info.stat
}

func (s *exportUnexportTestSuite) mockChip(c *C, name, path, label string, lines uint, stat *syscall.Stat_t) *gpio.ChardevChip {
	chip := gpio.MockChardevChip(path, name, label, lines)
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

func (s *exportUnexportTestSuite) checkAggregatedChipLines(c *C, lines strutil.Range) {
	configfsDir := filepath.Join(s.rootdir, "/sys/kernel/config/gpio-aggregator/snap.gadget-name.slot-name")
	c.Check(filepath.Join(configfsDir, "live"), testutil.FileEquals, "1")

	lineNum := 0
	for _, span := range lines {
		for line := span.Start; line <= span.End; line++ {
			c.Check(fmt.Sprintf("%s/line%d/key", configfsDir, lineNum), testutil.FileEquals, "label-2")
			c.Check(fmt.Sprintf("%s/line%d/offset", configfsDir, lineNum), testutil.FileEquals, strconv.FormatUint(uint64(line), 10))
			lineNum++
		}
	}
}

func (s *exportUnexportTestSuite) TestExportGadgetChardevChip(c *C) {
	s.mockChip(c, "gpiochip0", filepath.Join(s.rootdir, "/dev/gpiochip0"), "label-0", 3, nil)
	s.mockChip(c, "gpiochip1", filepath.Join(s.rootdir, "/dev/gpiochip1"), "label-1", 6, nil)
	s.mockChip(c, "gpiochip2", filepath.Join(s.rootdir, "/dev/gpiochip2"), "label-2", 9, nil)

	mknodCalled := 0
	restore := gpio.MockSyscallMknod(func(path string, mode uint32, dev int) (err error) {
		mknodCalled++
		c.Check(path, Equals, filepath.Join(s.rootdir, "/dev/snap/gpio-chardev/gadget-name/slot-name"))
		// permission bits are masked to zero
		c.Check(mode, Equals, uint32(unix.S_IFCHR))
		c.Check(unix.Major(uint64(dev)), Equals, uint32(254))
		c.Check(unix.Minor(uint64(dev)), Equals, uint32(3))
		s.mockChip(c, "gpiochip3", path, "gpio-aggregator.0", 7, &syscall.Stat_t{
			Rdev: unix.Mkdev(254, 3),
			Mode: syscall.S_IFCHR | 0600,
			Uid:  1001,
			Gid:  1002,
		})
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

	lines := strutil.Range{{Start: 0, End: 3}}
	err := gpio.ExportGadgetChardevChip(context.TODO(), []string{"label-2"}, lines, "gadget-name", "slot-name")
	c.Assert(err, IsNil)

	s.checkAggregatedChipLines(c, lines)

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

func (s *exportUnexportTestSuite) TestExportGadgetChardevChipContextCancellation(c *C) {
	s.mockChip(c, "gpiochip0", filepath.Join(s.rootdir, "/dev/gpiochip0"), "label-0", 3, nil)

	ctx, cancel := context.WithCancel(context.TODO())
	cancel()

	err := gpio.ExportGadgetChardevChip(ctx, []string{"label-0"}, strutil.Range{{Start: 0, End: 0}}, "gadget-name", "slot-name")
	c.Check(err, ErrorMatches, "cannot reload udev rules: context canceled")
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

	// Cleanup is triggered on error

	// Virtual device is removed
	c.Check(filepath.Join(s.rootdir, "/dev/snap/gpio-chardev/gadget-name/slot-name"), testutil.FileAbsent)
	// Udev rule is removed
	c.Check(filepath.Join(s.rootdir, "/run/udev/rules.d/69-snap.gadget-name.interface.gpio-chardev-slot-name.rules"), testutil.FileAbsent)
	// Aggregator device is deleted
	c.Check(s.mockChipInfos[filepath.Join(s.rootdir, "/dev/gpiochip3")], IsNil)
	c.Check(s.mockChipInfos[filepath.Join(s.rootdir, "/dev/snap/gpio-chardev/gadget-name/slot-name")], IsNil)
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
	exportedChipPath := filepath.Join(s.rootdir, "/dev/snap/gpio-chardev/gadget-name/slot-name")
	configfsDir := filepath.Join(s.rootdir, "/sys/kernel/config/gpio-aggregator/snap.gadget-name.slot-name")
	sysfsDir := filepath.Join(s.rootdir, "/sys/devices/platform/gpio-aggregator.0/gpiochip3")

	// Mock gadget slot virtual device
	c.Assert(os.MkdirAll(exportedChipPath, 0755), IsNil)
	// Mock udev rule
	udevRulePath := filepath.Join(s.rootdir, "/run/udev/rules.d/69-snap.gadget-name.interface.gpio-chardev-slot-name.rules")
	c.Assert(os.MkdirAll(filepath.Dir(udevRulePath), 0755), IsNil)
	c.Assert(os.WriteFile(udevRulePath, nil, 0644), IsNil)
	// Mock configfs directory
	c.Assert(os.MkdirAll(configfsDir, 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(configfsDir, "line0"), 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(configfsDir, "line1"), 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(configfsDir, "line2"), 0755), IsNil)
	// Mock sysfs directory
	c.Assert(os.MkdirAll(sysfsDir, 0755), IsNil)

	err := gpio.UnexportGadgetChardevChip("gadget-name", "slot-name")
	c.Assert(err, IsNil)

	// Virtual device is removed
	c.Check(exportedChipPath, testutil.FileAbsent)
	// Udev rule is removed
	c.Check(udevRulePath, testutil.FileAbsent)
	// Aggregator device is deleted
	c.Check(configfsDir, testutil.FileAbsent)
	c.Check(sysfsDir, testutil.FileAbsent)
}

func (s *exportUnexportTestSuite) TestExportUnexportGpioChardevRunthrough(c *C) {
	s.mockChip(c, "gpiochip0", filepath.Join(s.rootdir, "/dev/gpiochip0"), "label-0", 3, nil)

	aggregatedChipPath := filepath.Join(s.rootdir, "/dev/gpiochip3")
	slotDevicePath := filepath.Join(s.rootdir, "/dev/snap/gpio-chardev/gadget-name/slot-name")

	mknodCalled := 0
	restore := gpio.MockSyscallMknod(func(path string, mode uint32, dev int) (err error) {
		mknodCalled++
		c.Check(path, Equals, slotDevicePath)
		s.mockChip(c, "gpiochip3", path, "gpio-aggregator.0", 7, nil)
		return nil
	})
	defer restore()

	// 1. Export
	err := gpio.ExportGadgetChardevChip(context.TODO(), []string{"label-0"}, strutil.Range{{Start: 0, End: 2}}, "gadget-name", "slot-name")
	c.Assert(err, IsNil)

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

	// 2. Unexport
	err = gpio.UnexportGadgetChardevChip("gadget-name", "slot-name")
	c.Assert(err, IsNil)

	// Virtual device is removed
	c.Check(slotDevicePath, testutil.FileAbsent)
	// Udev rule is removed
	c.Check(udevRulePath, testutil.FileAbsent)
	// Aggregator device is deleted
	c.Check(s.mockChipInfos[aggregatedChipPath], IsNil)
	c.Check(s.mockChipInfos[slotDevicePath], IsNil)
}
