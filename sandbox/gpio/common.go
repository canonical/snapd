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
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
)

// chardevChip describes a gpio chardev device.
type chardevChip struct {
	path string
	// name is an identifier for the chip in the kernel (e.g. gpiochip3).
	name string
	// label is the name given to the chip through its driver used for matching (e.g. pinctrl-bcm2711).
	label string
	// numLines is the number of lines available on the gpio chip
	numLines uint
}

func (c *chardevChip) String() string {
	return fmt.Sprintf("(name: %s, label: %s, lines: %d)", c.name, c.label, c.numLines)
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

var getChardevChipInfo = func(path string) (*chardevChip, error) {
	kci, err := ioctlGetChipInfo(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read gpio chip info from %q: %w", path, err)
	}

	chip := &chardevChip{
		path:     path,
		name:     string(bytes.TrimRight(kci.name[:], "\x00")),
		label:    string(bytes.TrimRight(kci.label[:], "\x00")),
		numLines: uint(kci.lines),
	}
	return chip, nil
}

const (
	aggregatorDriverDir   = "/sys/bus/platform/drivers/gpio-aggregator"
	aggregatorConfigfsDir = "/sys/kernel/config/gpio-aggregator"
	ephemeralUdevRulesDir = "/run/udev/rules.d"
)

func snapConfigfsDir(instanceName, slotName string) string {
	return filepath.Join(dirs.GlobalRootDir, aggregatorConfigfsDir, fmt.Sprintf("snap.%s.%s", instanceName, slotName))
}

var osMkdir = os.Mkdir
var osStat = os.Stat
var osChmod = os.Chmod
var osChown = os.Chown
var osWriteFile = os.WriteFile
var syscallMknod = syscall.Mknod

func addAggregatedChip(sourceChipLabel string, lines strutil.Range, instanceName, slotName string) (chipName string, err error) {
	configfsBaseDir := snapConfigfsDir(instanceName, slotName)
	if err = osMkdir(configfsBaseDir, 0755); err != nil {
		return "", err
	}
	devName, err := os.ReadFile(filepath.Join(configfsBaseDir, "dev_name"))
	if err != nil {
		return "", err
	}
	devNameCleaned := strings.ReplaceAll(string(devName), "\n", "")

	// this assumes lines are sorted
	lineNum := 0
	for _, span := range lines {
		for line := span.Start; line <= span.End; line++ {
			lineDir := filepath.Join(configfsBaseDir, fmt.Sprintf("line%d", lineNum))
			if err = os.Mkdir(lineDir, 0755); err != nil {
				return "", err
			}
			if err = os.WriteFile(filepath.Join(lineDir, "key"), []byte(sourceChipLabel), 0644); err != nil {
				return "", err
			}
			if err = os.WriteFile(filepath.Join(lineDir, "offset"), []byte(strconv.FormatUint(uint64(line), 10)), 0644); err != nil {
				return "", err
			}
			lineNum++
		}
	}

	if err = os.WriteFile(filepath.Join(configfsBaseDir, "live"), []byte("1"), 0644); err != nil {
		return "", err
	}

	sysfsBaseDir := filepath.Join(dirs.GlobalRootDir, "/sys/devices/platform", devNameCleaned)
	entries, err := os.ReadDir(sysfsBaseDir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "gpiochip") {
			return entry.Name(), nil
		}
	}

	return "", fmt.Errorf("cannot find aggregated gpio chip device under %s", sysfsBaseDir)
}

func aggregatedChipUdevRulePath(instanceName, slotName string) string {
	fname := fmt.Sprintf("69-snap.%s.interface.gpio-chardev-%s.rules", instanceName, slotName)
	return filepath.Join(filepath.Join(dirs.GlobalRootDir, ephemeralUdevRulesDir), fname)
}

func addEphemeralUdevTaggingRule(ctx context.Context, chipName string, instanceName, slotName string) error {
	if err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, ephemeralUdevRulesDir), 0755); err != nil {
		return err
	}

	tag := fmt.Sprintf("snap_%s_interface_gpio_chardev_%s", instanceName, slotName)
	rule := fmt.Sprintf(`SUBSYSTEM=="gpio", KERNEL=="%s", TAG+="%s"`+"\n", chipName, tag)

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
	output, err = exec.CommandContext(ctx, "udevadm", "trigger", "--name-match", chipName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot trigger udev rules: %w", osutil.OutputErr(output, err))
	}

	return nil
}

func addGadgetSlotDevice(chipName, instanceName, slotName string) (err error) {
	fi, err := osStat(filepath.Join(dirs.DevDir, chipName))
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

func removeGadgetSlotDevice(instanceName, slotName string) (err error) {
	return os.RemoveAll(SnapChardevPath(instanceName, slotName))
}

func removeEphemeralUdevTaggingRule(instanceName, slotName string) error {
	path := aggregatedChipUdevRulePath(instanceName, slotName)
	return os.RemoveAll(path)
}

func removeAggregatedChip(instanceName, slotName string) error {
	configfsBaseDir := snapConfigfsDir(instanceName, slotName)

	if err := osWriteFile(filepath.Join(configfsBaseDir, "live"), []byte("0"), 0644); err != nil {
		return err
	}

	entries, err := os.ReadDir(configfsBaseDir)
	if err != nil {
		return err
	}

	// remove line directories
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(configfsBaseDir, entry.Name())); err != nil {
			return err
		}
	}

	return os.Remove(configfsBaseDir)
}

func validateLines(chip *chardevChip, lines strutil.Range) error {
	for _, span := range lines {
		if uint(span.End) >= chip.numLines {
			return fmt.Errorf("invalid line offset %d: line does not exist in %q", span.End, chip.name)
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

func findChips(filter func(chip *chardevChip) bool) ([]*chardevChip, error) {
	allPaths, err := filepath.Glob(filepath.Join(dirs.DevDir, "/gpiochip*"))
	if err != nil {
		return nil, err
	}

	var matched []*chardevChip
	for _, path := range allPaths {
		isAggregated, err := isAggregatedChip(path)
		if err != nil {
			return nil, err
		}
		if isAggregated {
			continue
		}
		chip, err := getChardevChipInfo(path)
		if err != nil {
			return nil, err
		}
		if filter(chip) {
			matched = append(matched, chip)
		}
	}

	return matched, nil
}
