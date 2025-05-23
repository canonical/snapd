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
	"os/signal"
	"strings"

	"github.com/snapcore/snapd/sandbox/gpio"
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

var gpioExportGadgetChardevChip = gpio.ExportGadgetChardevChip

func (c *cmdExportChardev) Execute(args []string) error {
	chipLabels := strings.Split(c.Args.ChipLabels, ",")
	lines, err := strutil.ParseRange(c.Args.Lines)
	if err != nil {
		return fmt.Errorf("invalid lines argument: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	return gpioExportGadgetChardevChip(ctx, chipLabels, lines, c.Args.Gadget, c.Args.Slot)
}
