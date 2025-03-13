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

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/inotify"
	"github.com/snapcore/snapd/strutil"
)

func init() {
	// Needed for testing.
	dirs.AddRootDirCallback(func(s string) {
		aggregatorLockPath = filepath.Join(s, "/sys/bus/platform/drivers/gpio-aggregator")
		aggregatorNewDevicePath = filepath.Join(s, "/sys/bus/platform/drivers/gpio-aggregator/new_device")
		ephermalUdevRulesDir = filepath.Join(s, "/run/udev/rules.d")
	})
}

var (
	aggregatorLockPath      = "/sys/bus/platform/drivers/gpio-aggregator"
	aggregatorNewDevicePath = "/sys/bus/platform/drivers/gpio-aggregator/new_device"
	ephermalUdevRulesDir    = "/run/udev/rules.d"
)

func lockAggregator() (unlocker func(), err error) {
	flock, err := osutil.OpenExistingLockForReading(aggregatorLockPath)
	if err != nil {
		return nil, err
	}
	if err := flock.Lock(); err != nil {
		return nil, err
	}
	return func() {
		flock.Close()
	}, nil
}

var aggregatorCreationTimeout = 120 * time.Second

func addAggregatedChip(sourceChip GPIOChardev, commaSeparatedLines string) (chip GPIOChardev, err error) {
	// Synchronize gpio helpers' access to the aggregator interface.
	unlocker, err := lockAggregator()
	if err != nil {
		return nil, err
	}
	defer unlocker()

	f, err := os.OpenFile(aggregatorNewDevicePath, os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	watcher, err := inotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	err = watcher.AddWatch(dirs.DevDir, inotify.InCreate)
	if err != nil {
		return nil, err
	}

	// <label> <lines>
	cmd := fmt.Sprintf("%s %s", sourceChip.Label(), commaSeparatedLines)
	if _, err = f.WriteString(cmd); err != nil {
		return nil, err
	}

	ctxWithTimeout, cancel := context.WithTimeout(context.Background(), aggregatorCreationTimeout)
	defer cancel()
	for {
		select {
		case event := <-watcher.Event:
			path := event.Name
			// Check prefix /dev/gpiochipX
			if strings.HasPrefix(path, filepath.Join(dirs.DevDir, "gpiochip")) {
				return getChipInfo(path)
			}
		case <-ctxWithTimeout.Done():
			return nil, fmt.Errorf("max timeout exceeded")
		}
	}
}

func addEphermalUdevTaggingRule(chip GPIOChardev, gadget, slot string) error {
	if err := os.MkdirAll(ephermalUdevRulesDir, 0755); err != nil {
		return err
	}

	tag := fmt.Sprintf("snap_%s_interface_gpio_chardev_%s", gadget, slot)
	rule := fmt.Sprintf("SUBSYSTEM==\"gpio\", KERNEL==\"%s\", TAG+=\"%s\"\n", chip.Name(), tag)

	fname := fmt.Sprintf("69-snap.%s.interface.gpio-chardev-%s.rules", gadget, slot)
	path := filepath.Join(ephermalUdevRulesDir, fname)
	if err := os.WriteFile(path, []byte(rule), 0644); err != nil {
		return err
	}

	// Make sure the rule we just dropped is loaded as sometimes it doesn't get
	// picked up right away.
	output, err := exec.Command("udevadm", "control", "--reload-rules").CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot reload udev rules: %s\nudev output:\n%s", err, string(output))
	}
	// Trigger the tagging rule.
	output, err = exec.Command("udevadm", "trigger", "--name-match", chip.Name()).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s\nudev output:\n%s", err, string(output))
	}

	return nil
}

var unixStat = unix.Stat
var unixMknod = unix.Mknod

func addGadgetSlotDevice(chip GPIOChardev, instanceName, slotName string) error {
	stat := &unix.Stat_t{}
	if err := unixStat(chip.Path(), stat); err != nil {
		return err
	}

	devPath := gadget.SnapGpioChardevPath(instanceName, slotName)
	if err := os.MkdirAll(filepath.Dir(devPath), 0755); err != nil {
		return err
	}
	if err := unixMknod(devPath, stat.Mode, int(stat.Rdev)); err != nil {
		return err
	}

	return nil
}

func validateLines(chip GPIOChardev, linesArg string) error {
	r, err := strutil.ParseRange(linesArg)
	if err != nil {
		return err
	}

	for _, span := range r.Spans {
		if uint(span.End) >= chip.NumLines() {
			return fmt.Errorf("invalid line offset %d: line does not exist in %q", span.End, chip.Name())
		}
	}

	return nil
}
