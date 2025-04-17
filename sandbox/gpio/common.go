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

package gpio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/inotify"
	"github.com/snapcore/snapd/strutil"
)

// ChardevChip describes a gpio chardev device.
type ChardevChip struct {
	Path string
	// Name is an identifier for the chip in the kernel (e.g. gpiochip3).
	Name string
	// Label is the name given to the chip through its driver used for matching (e.g. pinctrl-bcm2711).
	Label string
	// NumLines is the number of lines available on the gpio chip
	NumLines uint
}

func (c *ChardevChip) String() string {
	return fmt.Sprintf("(name: %s, label: %s, lines: %d)", c.Name, c.Label, c.NumLines)
}

// This has to match the memory layout of `struct gpiochip_info` found
// in include/uapi/linux/gpio.h in the kernel source tree.
type kernelChipInfo struct {
	name, label [32]byte
	lines       uint32
}

const _GPIO_GET_CHIPINFO_IOCTL uintptr = 0x8044b401

var unixSyscall = unix.Syscall

var ioctlGetChipInfo = func(path string) (*kernelChipInfo, error) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	conn, err := f.SyscallConn()
	if err != nil {
		return nil, err
	}

	kci := new(kernelChipInfo)
	var errno syscall.Errno
	err = conn.Control(func(fd uintptr) {
		_, _, errno = unixSyscall(unix.SYS_IOCTL, f.Fd(), _GPIO_GET_CHIPINFO_IOCTL, uintptr(unsafe.Pointer(kci)))
	})
	if errno != 0 {
		return nil, errno
	}
	return kci, err
}

var chardevChipInfo = func(path string) (*ChardevChip, error) {
	kci, err := ioctlGetChipInfo(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read gpio chip info from %q: %w", path, err)
	}

	chip := &ChardevChip{
		Path:     path,
		Name:     string(bytes.TrimRight(kci.name[:], "\x00")),
		Label:    string(bytes.TrimRight(kci.label[:], "\x00")),
		NumLines: uint(kci.lines),
	}
	return chip, nil
}

const (
	aggregatorDriverDir        = "/sys/bus/platform/drivers/gpio-aggregator"
	aggregatorNewDevicePath    = "/sys/bus/platform/drivers/gpio-aggregator/new_device"
	aggregatorDeleteDevicePath = "/sys/bus/platform/drivers/gpio-aggregator/delete_device"
	ephemeralUdevRulesDir      = "/run/udev/rules.d"
)

var lockAggregator = func() (unlocker func(), err error) {
	flock, err := osutil.OpenExistingLockForReading(filepath.Join(dirs.GlobalRootDir, aggregatorDriverDir))
	if err != nil {
		return nil, err
	}
	if err := flock.Lock(); err != nil {
		return nil, err
	}
	return func() {
		// we don't care about the error, this is best-effort.
		_ = flock.Close()
	}, nil
}

var aggregatorCreationTimeout = 120 * time.Second

func addAggregatedChip(ctx context.Context, sourceChip *ChardevChip, lines strutil.Range) (chip *ChardevChip, err error) {
	// synchronize gpio helpers' access to the aggregator interface
	unlocker, err := lockAggregator()
	if err != nil {
		return nil, err
	}
	defer unlocker()

	watcher, err := inotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	defer watcher.Close()

	err = watcher.AddWatch(dirs.DevDir, inotify.InCreate)
	if err != nil {
		return nil, err
	}

	// <label> <lines>
	cmd := fmt.Sprintf("%s %s", sourceChip.Label, lines.String())
	if err := os.WriteFile(filepath.Join(dirs.GlobalRootDir, aggregatorNewDevicePath), []byte(cmd), 0644); err != nil {
		return nil, err
	}

	timeoutTimer := time.NewTimer(aggregatorCreationTimeout)
	defer timeoutTimer.Stop()
	for {
		select {
		case event := <-watcher.Event:
			path := event.Name
			// check prefix /dev/gpiochipX
			if strings.HasPrefix(path, filepath.Join(dirs.DevDir, "gpiochip")) {
				return chardevChipInfo(path)
			}
		case <-timeoutTimer.C:
			return nil, errors.New("timeout waiting for aggregator device to appear")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func aggregatedChipUdevRulePath(instanceName, slotName string) string {
	fname := fmt.Sprintf("69-snap.%s.interface.gpio-chardev-%s.rules", instanceName, slotName)
	return filepath.Join(filepath.Join(dirs.GlobalRootDir, ephemeralUdevRulesDir), fname)
}

func addEphemeralUdevTaggingRule(ctx context.Context, chip *ChardevChip, instanceName, slotName string) error {
	if err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, ephemeralUdevRulesDir), 0755); err != nil {
		return err
	}

	tag := fmt.Sprintf("snap_%s_interface_gpio_chardev_%s", instanceName, slotName)
	rule := fmt.Sprintf(`SUBSYSTEM=="gpio", KERNEL=="%s", TAG+="%s"`+"\n", chip.Name, tag)

	path := aggregatedChipUdevRulePath(instanceName, slotName)
	if err := os.WriteFile(path, []byte(rule), 0644); err != nil {
		return err
	}

	// make sure the rule we just dropped is loaded as sometimes it doesn't get
	// picked up right away
	output, err := exec.CommandContext(ctx, "udevadm", "control", "--reload-rules").CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot reload udev rules: %w", osutil.OutputErr(output, err))
	}
	// trigger the tagging rule
	output, err = exec.CommandContext(ctx, "udevadm", "trigger", "--name-match", chip.Name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot trigger udev rules: %w", osutil.OutputErr(output, err))
	}

	return nil
}

var osStat = os.Stat
var osChmod = os.Chmod
var osChown = os.Chown
var syscallMknod = syscall.Mknod

func addGadgetSlotDevice(chip *ChardevChip, instanceName, slotName string) (err error) {
	fi, err := osStat(chip.Path)
	if err != nil {
		return err
	}

	stat, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return errors.New("internal error, expected os.File.FileInfo.Sys to return *syscall.Stat_t")
	}

	devPath := SnapChardevPath(instanceName, slotName)
	if err := os.MkdirAll(filepath.Dir(devPath), 0755); err != nil {
		return err
	}

	// create a character device node for the slot, with major/minor numbers
	// corresponding to the newly created aggregator device
	mode := uint32(stat.Mode) & (^uint32(0777))
	if err := syscallMknod(devPath, mode, int(stat.Rdev)); err != nil {
		return err
	}

	defer func() {
		if err != nil {
			// cleanup created device node
			os.RemoveAll(devPath)
		}
	}()

	// restore ownership
	if err := osChown(devPath, int(stat.Uid), int(stat.Gid)); err != nil {
		return err
	}
	// and original permission bits
	return osChmod(devPath, fi.Mode())
}

func removeGadgetSlotDevice(instanceName, slotName string) (aggregatedChip *ChardevChip, err error) {
	devPath := SnapChardevPath(instanceName, slotName)
	aggregatedChip, err = chardevChipInfo(devPath)
	if err != nil {
		return nil, err
	}
	return aggregatedChip, os.RemoveAll(devPath)
}

func removeEphemeralUdevTaggingRule(gadget, slot string) error {
	path := aggregatedChipUdevRulePath(gadget, slot)
	return os.RemoveAll(path)
}

func removeAggregatedChip(aggregatedChip *ChardevChip) error {
	// synchronize gpio helpers' access to the aggregator interface
	unlocker, err := lockAggregator()
	if err != nil {
		return err
	}
	defer unlocker()

	return os.WriteFile(filepath.Join(dirs.GlobalRootDir, aggregatorDeleteDevicePath), []byte(aggregatedChip.Label), 0644)
}

func validateLines(chip *ChardevChip, lines strutil.Range) error {
	for _, span := range lines {
		if uint(span.End) >= chip.NumLines {
			return fmt.Errorf("invalid line offset %d: line does not exist in %q", span.End, chip.Name)
		}
	}

	return nil
}

func isAggregatedChip(path string) (bool, error) {
	finfo, err := osStat(path)
	if err != nil {
		return false, err
	}
	stat, ok := finfo.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return false, errors.New("internal error")
	}

	maj, min := unix.Major(uint64(stat.Rdev)), unix.Minor(uint64(stat.Rdev))

	// filepath.Join is not used to prevent cleaning the ".." as it is crucial
	// to follow the MAJ:MIN subdirectory symlink
	driverDir, err := filepath.EvalSymlinks(dirs.SysfsDir + fmt.Sprintf("/dev/char/%d:%d/../driver", maj, min))
	return driverDir == filepath.Join(dirs.GlobalRootDir, aggregatorDriverDir), err
}

func findChips(filter func(chip *ChardevChip) bool) ([]*ChardevChip, error) {
	allPaths, err := filepath.Glob(filepath.Join(dirs.DevDir, "/gpiochip*"))
	if err != nil {
		return nil, err
	}

	var matched []*ChardevChip
	for _, path := range allPaths {
		isAggregated, err := isAggregatedChip(path)
		if err != nil {
			return nil, err
		}
		if isAggregated {
			continue
		}
		chip, err := chardevChipInfo(path)
		if err != nil {
			return nil, err
		}
		if filter(chip) {
			matched = append(matched, chip)
		}
	}

	return matched, nil
}
