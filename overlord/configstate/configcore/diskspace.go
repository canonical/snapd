// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

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

package configcore

import (
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/overlord/configstate/config"
)

const defaultDiskSpaceReservation = uint64(5 * quantity.SizeMiB)

// legacyDiskSpaceFeatures are the experimental flags superseded by
// disk-reservation.size.
var legacyDiskSpaceFeatures = []features.SnapdFeature{
	features.CheckDiskSpaceInstall,
	features.CheckDiskSpaceRefresh,
	features.CheckDiskSpaceRemove,
}

func init() {
	supportedConfigurations["core.disk-reservation.size"] = true
}

// MigrateDiskSpaceReservation converts the legacy experimental feature flags
// into disk-reservation.size and then retires the flags, so the option stays
// authoritative. It can be removed with those flags in a later release.
func MigrateDiskSpaceReservation(tr RunTransaction) error {
	enabled, err := legacyDiskSpaceFeatureEnabled(tr)
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}

	var reservation any
	err = tr.Get("core", "disk-reservation.size", &reservation)
	switch {
	case config.IsNoOption(err):
		// only seed the default when the option is not configured already
		if err := tr.Set("core", "disk-reservation.size", defaultDiskSpaceReservation); err != nil {
			return err
		}
	case err != nil:
		return err
	}

	for _, feature := range legacyDiskSpaceFeatures {
		snapName, confName := feature.ConfigOption()
		if err := tr.Set(snapName, confName, nil); err != nil {
			return err
		}
	}

	return nil
}

func legacyDiskSpaceFeatureEnabled(tr RunTransaction) (bool, error) {
	for _, feature := range legacyDiskSpaceFeatures {
		enabled, err := features.Flag(tr, feature)
		if err != nil {
			return false, err
		}
		if enabled {
			return true, nil
		}
	}

	return false, nil
}

// handleDiskSpaceReservation runs the migration when a legacy experimental flag
// is toggled at runtime, so the result matches what a snapd restart would do.
func handleDiskSpaceReservation(tr RunTransaction, opts *fsOnlyContext) error {
	// Only react to the legacy flags, otherwise an explicit unset of
	// disk-reservation.size would immediately be migrated back.
	if !changesLegacyDiskSpaceFeature(tr.Changes()) {
		return nil
	}

	return MigrateDiskSpaceReservation(tr)
}

func changesLegacyDiskSpaceFeature(changes []string) bool {
	for _, feature := range legacyDiskSpaceFeatures {
		snapName, confName := feature.ConfigOption()
		for _, change := range changes {
			if change == snapName+"."+confName {
				return true
			}
		}
	}

	return false
}

func validateDiskSpaceReservation(tr RunTransaction) error {
	reservation, err := coreCfg(tr, "disk-reservation.size")
	if err != nil {
		return err
	}
	if reservation == "" {
		return nil
	}
	_, err = quantity.ParseSize(reservation)
	return err
}
