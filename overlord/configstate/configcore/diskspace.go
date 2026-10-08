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
	"errors"

	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/strutil"
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

// MigrateDiskSpaceReservation seeds the reservation while preserving flags for rollback.
func MigrateDiskSpaceReservation(tr RunTransaction) error {
	var migrated bool
	if err := tr.State().Get("disk-space-reservation-migrated", &migrated); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if migrated {
		return nil
	}

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
	if strutil.ListContains(tr.Changes(), "core.disk-reservation.size") || !changesLegacyDiskSpaceFeature(tr.Changes()) {
		return nil
	}

	st := tr.State()
	st.Lock()
	defer st.Unlock()
	return MigrateDiskSpaceReservation(tr)
}

func completeDiskSpaceReservationMigration(tr RunTransaction) error {
	if !strutil.ListContains(tr.Changes(), "core.disk-reservation.size") {
		return nil
	}

	st := tr.State()
	st.Lock()
	defer st.Unlock()
	tr.Commit()
	st.Set("disk-space-reservation-migrated", true)
	return nil
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
