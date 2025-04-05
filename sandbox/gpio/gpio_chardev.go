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
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
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
		return errors.New("more than one gpio chips were found matching chip labels")
	}

	chip := chips[0]
	if err := validateLines(chip, lines); err != nil {
		return fmt.Errorf("invalid lines argument: %w", err)
	}

	aggregatedChip, err := addAggregatedChip(ctx, chip, lines)
	if err != nil {
		return err
	}
	if err := addEphermalUdevTaggingRule(ctx, aggregatedChip, gadgetName, slotName); err != nil {
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
	if err := removeEphermalUdevTaggingRule(gadgetName, slotName); err != nil {
		return err
	}
	return removeAggregatedChip(aggregatedChip)
}
