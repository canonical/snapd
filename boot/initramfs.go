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

// InitramfsRunModeSelectSnapsToMount returns a map of the snap paths to mount
// for the specified snap types.
func InitramfsRunModeSelectSnapsToMount(
	typs []snap.Type,
	modeenv *Modeenv,
) (map[snap.Type]snap.PlaceInfo, error) {
	var sn snap.PlaceInfo
	var err error
	m := make(map[snap.Type]snap.PlaceInfo)
	for _, typ := range typs {
		var selectSnapFn func() (snap.PlaceInfo, error)
		switch typ {
		case snap.TypeBase:
			bs := &bootState20Base{}
			bs.modeenv = modeenv
			selectSnapFn = bs.selectAndCommitSnapInitramfsMount
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
			selectSnapFn = bs.selectAndCommitSnapInitramfsMount
		}
		sn, err = selectSnapFn()
		if err != nil {
			return nil, err
		}

		m[typ] = sn
	}

	return m, nil
}
