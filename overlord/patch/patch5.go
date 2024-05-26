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
	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/timings"
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
//   - regenerate generated .service files
func patch5(st *state.State) error {
	log := log{}

	snapStates := mylog.Check2(snapstate.All(st))

	// create timings to satisfy StartServices/StopServices API, but don't save them
	tm := timings.New(nil)
	for snapName, snapst := range snapStates {
		if !snapst.Active {
			continue
		}

		info := mylog.Check2(snapst.CurrentInfo())

		svcs := info.Services()
		if len(svcs) == 0 {
			logger.Debugf("patch 5: skipping for %q: no services", snapName)
			continue
		}
		mylog.Check(wrappers.StopServices(svcs, nil, snap.StopReasonRefresh, log, tm))
		mylog.Check(wrappers.EnsureSnapServices(map[*snap.Info]*wrappers.SnapServiceOptions{
			info: nil,
		}, nil, nil, log))
		mylog.Check(wrappers.StartServices(svcs, nil, nil, log, tm))

		logger.Noticef("patch 5: %q updated", snapName)
	}

	return nil
}
