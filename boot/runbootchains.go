// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package boot

import (
	"fmt"
	"path/filepath"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
)

// GetRunBootChain returns the boot chain expected to be used
// for a normal "run" mode boot.
//
// The image files in the bootchain will either point a file in a snap
// or to a file in the trusted boot asset cache. They will not
// point to the effective path where the read from, though they
// are expected to be the same, unless boot partition were compromised.
func GetRunBootChain(modeenv *Modeenv) ([]bootloader.BootFile, error) {
	rbl, err := bootloaderFind(InitramfsUbuntuSeedDir, &bootloader.Options{
		Role: bootloader.RoleRecovery,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot find recovery bootloader: %w", err)
	}

	tbl, ok := rbl.(bootloader.TrustedAssetsBootloader)
	if !ok {
		return nil, fmt.Errorf("internal error: recovery bootloader does not support trusted assets")
	}

	bl, err := bootloaderFind(InitramfsUbuntuBootDir, &bootloader.Options{
		Role:        bootloader.RoleRunMode,
		NoSlashBoot: true,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot find run bootloader: %w", err)
	}

	ebl, ok := bl.(bootloader.ExtractedRunKernelImageBootloader)
	if !ok {
		return nil, fmt.Errorf("internal error: run bootloader does not support kernel extraction")
	}

	info, err := ebl.TryKernel()
	if err != nil {
		if err == bootloader.ErrNoTryKernelRef {
			info, err = ebl.Kernel()
		}
		if err != nil {
			return nil, err
		}
	}

	kernelPath := info.MountFile()

	trustedAssets, err := tbl.TrustedAssets()
	if err != nil {
		return nil, err
	}

	runModeBootChains, err := tbl.BootChains(bl, kernelPath)
	if err != nil {
		return nil, err
	}

	// runModeBootChains is all possible run boot chains, but only one should exist (there
	// are legacy boot chains before we registered UEFI boot entries).
	// The "BootFile"s for the gadget part points to identifier names instead of real path, so we
	// need to resolve those. To resolve those we need to cross check with the modeenv, and then
	// find the file in the cache. The last one, is the kernel and should be pointing to the right place.
	for _, runModeBootChain := range runModeBootChains {
		var chain []bootloader.BootFile

		if len(runModeBootChain) == 0 {
			// That is not possible for a boot chain to be size 0, because that would mean there is no
			// kernel. We should not ignore this, there are bigger problems.
			return nil, fmt.Errorf("internal error: no file in boot chain")
		}

		ignoreChain := false
		for _, bf := range runModeBootChain[:len(runModeBootChain)-1] {
			path := bf.Path
			name, ok := trustedAssets[path]
			if !ok {
				return nil, fmt.Errorf("internal error: unknown trusted asset %s from boot chain", path)
			}
			var hashes []string
			if bf.Role == bootloader.RoleRecovery {
				hashes, ok = modeenv.CurrentTrustedRecoveryBootAssets[name]
			} else {
				hashes, ok = modeenv.CurrentTrustedBootAssets[name]
			}
			if !ok {
				ignoreChain = true
				break
			}

			// In theory we should only have one hash here. Multiple would be when we are trying
			// a boot chain, and this should have been cleaned. It should be safe to take the last one (newest).
			if len(hashes) > 1 {
				logger.Noticef("WARNING: multiple hashes for a trusted boot file were found.")
			}
			hash := hashes[len(hashes)-1]
			p := filepath.Join(dirs.SnapBootAssetsDir, bl.Name(), fmt.Sprintf("%s-%s", name, hash))
			chain = append(chain, bootloader.NewBootFile("", p, bf.Role))
		}
		if !ignoreChain {
			return append(chain, runModeBootChain[len(runModeBootChain)-1]), nil
		}
	}

	return nil, fmt.Errorf("cannot find the active boot chain")
}
