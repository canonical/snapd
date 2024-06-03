// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build nobootloader

/*
 * Copyright (C) 2014-2024 Canonical Ltd
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

package bootloader

import (
	"errors"
)

var errBootloaderSupportDisabled = errors.New("no bootloader support")

var (
	// no bootloader supported with this build configuration
	bootloaders = []bootloaderNewFunc{}
)

// Find returns the bootloader for the system
// or an error if no bootloader is found.
//
// The rootdir option is useful for image creation operations. It
// can also be used to find the recovery bootloader, e.g. on uc20:
//
//	bootloader.Find("/run/mnt/ubuntu-seed")
func Find(rootdir string, opts *Options) (Bootloader, error) {
	return nil, errBootloaderSupportDisabled
}

// ForGadget returns a bootloader matching a given gadget by inspecting the
// contents of gadget directory or an error if no matching bootloader is found.
func ForGadget(gadgetDir, rootDir string, opts *Options) (Bootloader, error) {
	return nil, errBootloaderSupportDisabled
}
