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
	"fmt"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/timings"
)

// TODO:UC20: add install mode mounts code too

// InitramfsRunModeSnapsToMount returns a map of the snap paths to mount for the
// specified snap types.
func InitramfsRunModeSnapsToMount(typs []snap.Type) (map[snap.Type]string, error) {
	var choices bootStateUpdate
	var sn snap.PlaceInfo
	var err error
	m := make(map[snap.Type]string)
	for _, typ := range typs {
		ebs := newEarlyBootState20(typ)
		sn, choices, err = ebs.chooseSnapInitramfsMount(choices)
		if err != nil {
			return nil, err
		}

		snapPath := filepath.Join(dirs.EarlyBootUbuntuData, "system-data", dirs.SnapBlobDir, sn.Filename())
		m[typ] = snapPath
	}

	// if we have something to actually commit, then commit it
	if choices != nil {
		err = choices.commit()
		if err != nil {
			return nil, err
		}
	}

	return m, nil
}

// TODO: move this into it's own file?
func recoverySystemEssentialSnaps(seedDir, recoverySystem string, essentialTypes []snap.Type) ([]*seed.Snap, error) {
	systemSeed, err := seed.Open(seedDir, recoverySystem)
	if err != nil {
		return nil, err
	}

	seed20, ok := systemSeed.(seed.EssentialMetaLoaderSeed)
	if !ok {
		return nil, fmt.Errorf("internal error: UC20 seed must implement EssentialMetaLoaderSeed")
	}

	// load assertions into a temporary database
	if err := systemSeed.LoadAssertions(nil, nil); err != nil {
		return nil, err
	}

	// load and verify metadata only for the relevant essential snaps
	perf := timings.New(nil)
	if err := seed20.LoadEssentialMeta(essentialTypes, perf); err != nil {
		return nil, err
	}

	return seed20.EssentialSnaps(), nil
}
