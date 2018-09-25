// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package patch

import (
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/wrappers"
)

func init() {
	patches[5] = []PatchFunc{patch5}
}

type log struct{}

func (log) Notify(status string) {
	logger.Noticef("patch 5: %s", status)
}

// patch5:
//  - regenerate generated .service files
func patch5(st *state.State) error {
	log := log{}

	snapStates, err := snapstate.All(st)
	if err != nil {
		return err
	}

	for snapName, snapst := range snapStates {
		if !snapst.Active {
			continue
		}

		info, err := snapst.CurrentInfo()
		if err != nil {
			return err
		}

		svcs := info.Services()
		if len(svcs) == 0 {
			logger.Debugf("patch 5: skipping for %q: no services", snapName)
			continue
		}

		err = wrappers.StopServices(svcs, snap.StopReasonRefresh, log)
		if err != nil {
			return err
		}

		err = wrappers.AddSnapServices(info, log)
		if err != nil {
			return err
		}

		err = wrappers.StartServices(svcs, log)
		if err != nil {
			return err
		}

		logger.Noticef("patch 5: %q updated", snapName)
	}

	return nil
}
