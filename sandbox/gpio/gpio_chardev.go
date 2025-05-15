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
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil/kmod"
	"github.com/snapcore/snapd/strutil"
)

// SnapChardevPath returns the path for the exported snap-specific gpio
// chardev chip device node based on the plug/slot name.
func SnapChardevPath(instanceName, plugOrSlot string) string {
	return filepath.Join(dirs.SnapGpioChardevDir, instanceName, plugOrSlot)
}

// ExportGadgetChardevChip exports specified gpio chip lines through a
// gpio aggregator for a given gadget gpio-chardev interface slot.
//
// Note: chipLabels must match exactly one chip.
func ExportGadgetChardevChip(ctx context.Context, chipLabels []string, lines strutil.Range, gadgetName, slotName string) error {
	// The filtering is quadratic, but we only expect a few chip
	// labels, so it is fine.
	filter := func(chip *ChardevChip) bool {
		return strutil.ListContains(chipLabels, chip.Label)
	}
	chips, err := findChips(filter)
	if err != nil {
		return err
	}
	if len(chips) == 0 {
		return errors.New("no matching gpio chips found matching chip labels")
	}
	if len(chips) > 1 {
		concat := chips[0].Label
		for _, chip := range chips[1:] {
			concat += " " + chip.Label
		}
		return fmt.Errorf("more than one gpio chips were found matching chip labels (%s)", concat)
	}

	chip := chips[0]
	if err := validateLines(chip, lines); err != nil {
		return fmt.Errorf("invalid lines argument: %w", err)
	}

	// Order of operations below is important because the exported gpio
	// aggregator device doesn't have enough metadata for udev to match in
	// advance. Instead, We use the dynamically generated chip name is used
	// for matching e.g. `SUBSYSTEM=="gpio", KERNEL=="gpiochip3"`.
	aggregatedChip, err := addAggregatedChip(ctx, chip, lines)
	if err != nil {
		return err
	}
	if err := addEphemeralUdevTaggingRule(ctx, aggregatedChip, gadgetName, slotName); err != nil {
		return err
	}
	return addGadgetSlotDevice(aggregatedChip, gadgetName, slotName)
}

// UnexportGadgetChardevChip unexports previously exported gpio chip lines
// for a given gadget gpio-chardev interface slot.
func UnexportGadgetChardevChip(gadgetName, slotName string) error {
	aggregatedChip, err := removeGadgetSlotDevice(gadgetName, slotName)
	if err != nil {
		return err
	}
	if err := removeEphemeralUdevTaggingRule(gadgetName, slotName); err != nil {
		return err
	}
	return removeAggregatedChip(aggregatedChip)
}

var kmodLoadModule = kmod.LoadModule

// EnsureAggregatorDriver attempts to load the gpio-aggregator kernel
// module iff it was not already loaded and checks if the configfs
// interface for gpio-aggregator is available.
func EnsureAggregatorDriver() error {
	_, err := os.Stat(filepath.Join(dirs.GlobalRootDir, aggregatorDriverDir))
	if errors.Is(err, os.ErrNotExist) {
		if err := kmodLoadModule("gpio-aggregator", nil); err != nil {
			return err
		}
	}

	return CheckConfigfsSupport()
}

// CheckConfigfsSupport checks if the configfs interface for
// gpio-aggregator is available.
func CheckConfigfsSupport() error {
	// GPIO chardev support is hidden until kernel configfs gpio-aggregator interface
	// makes it to the 24.04 kernel AND the snap-gpio-helper is updated to use the
	// new configfs interface.
	// https://bugs.launchpad.net/ubuntu/+source/linux/+bug/2103496

	// The check should be as simple as checking that /sys/kernel/config/gpio-aggregator exists.
	return errors.New("gpio-aggregator configfs support is missing")
}
