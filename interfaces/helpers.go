// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package interfaces

import (
	"fmt"

	"github.com/snapcore/snapd/timings"
)

// SetupMany generates profiles of snaps using either SetupMany() method of the security backend (if implemented), or Setup(). All errors are logged.
// The return value indicates if all profiles were successfully generated.
func SetupMany(repo *Repository, backend SecurityBackend, appSets []*SnapAppSet,
	confinementOpts func(snapName string) ConfinementOptions,
	setupCtx func(snapName string) SetupContext,
	tm timings.Measurer,
) []error {
	var errors []error
	// use .SetupMany() if implemented by the backend, otherwise fall back to .Setup()
	if setupManyInterface, ok := backend.(SecurityBackendSetupMany); ok {
		timings.Run(tm, "setup-security-backend[many]", fmt.Sprintf("setup security backend %q for %d snaps", backend.Name(), len(appSets)), func(nesttm timings.Measurer) {
			errors = setupManyInterface.SetupMany(appSets, confinementOpts, setupCtx, repo, nesttm)
		})
	} else {
		// For each snap:
		for _, set := range appSets {
			snapInfo := set.Info()
			snapName := snapInfo.InstanceName()
			// Compute confinement options
			opts := confinementOpts(snapName)
			sctx := setupCtx(snapName)

			// Refresh security of this snap and backend
			timings.Run(tm, "setup-security-backend", fmt.Sprintf("setup security backend %q for snap %q", backend.Name(), snapInfo.InstanceName()), func(nesttm timings.Measurer) {
				if err := backend.Setup(set, opts, sctx, repo, nesttm); err != nil {
					errors = append(errors, err)
				}
			})
		}
	}
	return errors
}

// ApplyDelayedEffects delayed side effects of a previous call to backend's Setup().
func ApplyDelayedEffects(repo *Repository, backend SecurityBackend, appSet *SnapAppSet, effects []DelayedSideEffect, tm timings.Measurer) error {
	if len(effects) == 0 {
		return nil
	}

	delayedEffectsBackend, ok := backend.(DelayedSideEffectsBackend)
	if !ok {
		return fmt.Errorf("internal error: calling apply delayed effects for unsupported backend %q", backend.Name())
	}

	// Refresh security of this snap and backend
	var err error
	timings.Run(tm, "delayed-setup-security-backend",
		fmt.Sprintf("delayed setup security backend %q effects for snap %q", backend.Name(), appSet.InstanceName()),
		func(nesttm timings.Measurer) {
			err = delayedEffectsBackend.ApplyDelayedEffects(appSet, effects, nesttm)
		})
	return err
}
