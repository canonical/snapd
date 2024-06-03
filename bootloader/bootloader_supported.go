// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nobootloader

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
	"fmt"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

var (
	//  bootloaders list all possible bootloaders by their constructor
	//  function.
	bootloaders = []bootloaderNewFunc{
		newUboot,
		newGrub,
		newAndroidBoot,
		newLk,
		newPiboot,
	}
)

// Find returns the bootloader for the system
// or an error if no bootloader is found.
//
// The rootdir option is useful for image creation operations. It
// can also be used to find the recovery bootloader, e.g. on uc20:
//
//	bootloader.Find("/run/mnt/ubuntu-seed")
func Find(rootdir string, opts *Options) (Bootloader, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	if forcedBootloader != nil || forcedError != nil {
		return forcedBootloader, forcedError
	}

	if rootdir == "" {
		rootdir = dirs.GlobalRootDir
	}
	if opts == nil {
		opts = &Options{}
	}

	// note that the order of this is not deterministic
	for _, blNew := range bootloaders {
		bl := blNew(rootdir, opts)
		present, err := bl.Present()
		if err != nil {
			return nil, fmt.Errorf("bootloader %q found but not usable: %v", bl.Name(), err)
		}
		if present {
			return bl, nil
		}
	}
	// no, weeeee
	return nil, ErrBootloader
}

// ForGadget returns a bootloader matching a given gadget by inspecting the
// contents of gadget directory or an error if no matching bootloader is found.
func ForGadget(gadgetDir, rootDir string, opts *Options) (Bootloader, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	if forcedBootloader != nil || forcedError != nil {
		return forcedBootloader, forcedError
	}
	for _, blNew := range bootloaders {
		bl := blNew(rootDir, opts)
		markerConf := filepath.Join(gadgetDir, bl.Name()+".conf")
		// do we have a marker file?
		if osutil.FileExists(markerConf) {
			return bl, nil
		}
	}
	return nil, ErrBootloader
}
