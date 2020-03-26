// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/snap"
)

// TODO:UC20: add install/recover mode mounts code here too?

// InitramfsRunModeChooseSnapsToMount returns a map of the snap paths to mount
// for the specified snap types.
func InitramfsRunModeChooseSnapsToMount(
	typs []snap.Type,
	modeenv *Modeenv,
) (map[snap.Type]snap.PlaceInfo, error) {
	var sn snap.PlaceInfo
	var err error
	m := make(map[snap.Type]snap.PlaceInfo)
	for _, typ := range typs {
		// TODO: it would be nice to use an interface here since the methods are
		// already organized this way...
		switch typ {
		case snap.TypeBase:
			bs := &bootState20Base{}
			bs.modeenv = modeenv
			sn, err = bs.chooseAndCommitSnapInitramfsMount()
			if err != nil {
				return nil, err
			}
		case snap.TypeKernel:
			blOpts := &bootloader.Options{NoSlashBoot: true}
			blDir := InitramfsUbuntuBootDir
			bs := &bootState20Kernel{
				blDir:  blDir,
				blOpts: blOpts,
				kModeenv: bootState20Modeenv{
					modeenv: modeenv,
				},
			}
			sn, err = bs.chooseAndCommitSnapInitramfsMount()
			if err != nil {
				return nil, err
			}
		}

		m[typ] = sn
	}

	return m, nil
}
