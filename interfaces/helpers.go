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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/timings"
)

// SetupMany generates profiles of snaps using either SetupMany() method of the security backend (if implemented), or Setup(). All errors are logged.
// The return value indicates if all profiles were successfully generated.
func SetupMany(repo *Repository, backend SecurityBackend, appSets []*SnapAppSet, confinementOpts func(snapName string) ConfinementOptions, tm timings.Measurer) []error {
	var errors []error
	// use .SetupMany() if implemented by the backend, otherwise fall back to .Setup()
	if setupManyInterface, ok := backend.(SecurityBackendSetupMany); ok {
		timings.Run(tm, "setup-security-backend[many]", fmt.Sprintf("setup security backend %q for %d snaps", backend.Name(), len(appSets)), func(nesttm timings.Measurer) {
			errors = setupManyInterface.SetupMany(appSets, confinementOpts, repo, nesttm)
		})
	} else {
		// For each snap:
		for _, set := range appSets {
			snapInfo := set.Info()
			snapName := snapInfo.InstanceName()
			// Compute confinement options
			opts := confinementOpts(snapName)

			// Refresh security of this snap and backend
			timings.Run(tm, "setup-security-backend", fmt.Sprintf("setup security backend %q for snap %q", backend.Name(), snapInfo.InstanceName()), func(nesttm timings.Measurer) {
				mylog.Check(backend.Setup(set, opts, repo, nesttm))
			})
		}
	}
	return errors
}
