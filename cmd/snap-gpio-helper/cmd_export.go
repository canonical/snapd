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
	"errors"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/strutil"
)

type cmdExportChardev struct {
	Args struct {
		ChipLabels string `positional-arg-name:"<gpio-labels>" description:"comma-separated list of source chip label(s) to match"`
		Lines      string `positional-arg-name:"<lines>" description:"comma-separated list of target gpio line(s)"`
		Gadget     string `positional-arg-name:"<gadget>" description:"gadget snap name"`
		Slot       string `positional-arg-name:"<slot>" description:"gpio-chardev slot name"`
	} `positional-args:"yes" required:"true"`
}

func (c *cmdExportChardev) Execute(args []string) error {
	chipLabels := strings.Split(c.Args.ChipLabels, ",")
	filter := func(chip GPIOChardev) bool {
		return strutil.ListContains(chipLabels, chip.Label())
	}
	chips, err := findChips(filter)
	if err != nil {
		return err
	}
	if len(chips) == 0 {
		return errors.New("no matching gpio chips found matching passed labels")
	}
	if len(chips) > 1 {
		return errors.New("more than one gpio chips were found matching passed labels")
	}

	chip := chips[0]
	if err := validateLines(chip, c.Args.Lines); err != nil {
		return fmt.Errorf("invalid lines argument: %w", err)
	}

	aggregatedChip, err := addAggregatedChip(chip, c.Args.Lines)
	if err != nil {
		return fmt.Errorf("cannot add aggregator device: %w", err)
	}

	if err := addEphermalUdevTaggingRule(aggregatedChip, c.Args.Gadget, c.Args.Slot); err != nil {
		return fmt.Errorf("cannot add udev tagging rule: %w", err)
	}

	if err := addGadgetSlotDevice(aggregatedChip, c.Args.Gadget, c.Args.Slot); err != nil {
		return fmt.Errorf("cannot add gadget slot device: %w", err)
	}

	return nil
}
