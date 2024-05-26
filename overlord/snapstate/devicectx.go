// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2019 Canonical Ltd
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

package snapstate

import (
	"errors"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

// A DeviceContext provides for operating as a given device and with
// its brand store either for normal operation or over a remodeling.
type DeviceContext interface {
	// GroundContext returns a context corresponding to the
	// original model of the device for a remodel, or a context
	// equivalent to this one otherwise, except in both cases
	// Store cannot be used and must panic.
	GroundContext() DeviceContext

	// Store returns the store service to use under this context or nil if the snapstate store is appropriate.
	Store() StoreService

	// ForRemodeling returns whether this context is for use over a remodeling.
	ForRemodeling() bool

	// SystemMode returns the system  mode (run,install,recover,...).
	SystemMode() string

	// DeviceContext should be usable as snap.Device
	snap.Device
}

// Hook setup by devicestate to pick a device context from state,
// optional task or an optionally pre-provided one. It's expected to
// return ErrNoState if a model assertion is not yet known.
var (
	DeviceCtx func(st *state.State, task *state.Task, providedDeviceCtx DeviceContext) (DeviceContext, error)
)

// Hook setup by devicestate to know whether a remodeling is in progress.
var (
	RemodelingChange func(st *state.State) *state.Change
)

// ModelFromTask returns a model assertion through the device context for the task.
func ModelFromTask(task *state.Task) (*asserts.Model, error) {
	deviceCtx := mylog.Check2(DeviceCtx(task.State(), task, nil))

	return deviceCtx.Model(), nil
}

// DevicePastSeeding returns a device context if a model assertion is
// available and the device is seeded, at that point the device store
// is known and seeding done. Otherwise it returns a
// ChangeConflictError about being too early unless a pre-provided
// DeviceContext is passed in. It will again return a conflict error
// during remodeling unless the providedDeviceCtx is for it.
func DevicePastSeeding(st *state.State, providedDeviceCtx DeviceContext) (DeviceContext, error) {
	var seeded bool
	mylog.Check(st.Get("seeded", &seeded))
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	if chg := RemodelingChange(st); chg != nil {
		// a remodeling is in progress and this is not called
		// as part of it. The 2nd check should not be needed
		// in practice.
		if providedDeviceCtx == nil || !providedDeviceCtx.ForRemodeling() {
			return nil, &ChangeConflictError{
				Message: "remodeling in progress, no other " +
					"changes allowed until this is done",
				ChangeKind: "remodel",
				ChangeID:   chg.ID(),
			}
		}
	}
	devCtx := mylog.Check2(DeviceCtx(st, nil, providedDeviceCtx))
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	// when seeded devCtx should not be nil except in the rare
	// case of upgrades from a snapd before the introduction of
	// the fallback generic/generic-classic model
	if !seeded || devCtx == nil {
		return nil, &ChangeConflictError{
			Message: "too early for operation, device not yet" +
				" seeded or device model not acknowledged",
			ChangeKind: "seed",
		}
	}

	return devCtx, nil
}

// DeviceCtxFromState returns a device context if a model assertion is
// available. Otherwise it returns a ChangeConflictError about being
// too early unless an pre-provided DeviceContext is passed in.
func DeviceCtxFromState(st *state.State, providedDeviceCtx DeviceContext) (DeviceContext, error) {
	deviceCtx := mylog.Check2(DeviceCtx(st, nil, providedDeviceCtx))

	return deviceCtx, nil
}
