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
)

type cmdExportChardev struct {
	ChipLabels []string `long:"chip-label" description:"source chip label(s) to match" required:"yes"`
	Lines      []uint   `long:"line" description:"gpio line(s) to export" required:"yes"`
	Gadget     string   `long:"gadget" description:"gadget snap name" required:"yes"`
	Slot       string   `long:"slot" description:"gpio-chardev slot name" required:"yes"`
}

func (c *cmdExportChardev) Execute(args []string) error {
	return errors.New("not implemented")
}
