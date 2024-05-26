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

package daemon

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
)

var routineConsoleConfStartCmd = &Command{
	Path:        "/v2/internal/console-conf-start",
	POST:        consoleConfStartRoutine,
	WriteAccess: authenticatedAccess{},
}

var delayTime = 20 * time.Minute

// consoleConfStartRoutineResult is the result of running the console-conf start
// routine..
type consoleConfStartRoutineResult struct {
	ActiveAutoRefreshChanges []string `json:"active-auto-refreshes,omitempty"`
	ActiveAutoRefreshSnaps   []string `json:"active-auto-refresh-snaps,omitempty"`
}

func consoleConfStartRoutine(c *Command, r *http.Request, _ *auth.UserState) Response {
	// no body expected, error if we were provided anything
	defer r.Body.Close()
	var routineBody struct{}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&routineBody); err != nil && err != io.EOF {
		return BadRequest("cannot decode request body into console-conf operation: %v", err)
	}

	// now run the start routine first by trying to grab a lock on the refreshes
	// for all snaps, which fails if there are any active changes refreshing
	// snaps
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	snapAutoRefreshChanges := mylog.Check2(c.d.overlord.SnapManager().EnsureAutoRefreshesAreDelayed(delayTime))

	logger.Debugf("Ensured that new auto refreshes are delayed by %s to allow console-conf to run", delayTime)

	if len(snapAutoRefreshChanges) == 0 {
		// no changes yet, and we delayed the refresh successfully so
		// console-conf is okay to run normally
		return SyncResponse(&consoleConfStartRoutineResult{})
	}

	chgIds := make([]string, 0, len(snapAutoRefreshChanges))
	snapNames := make([]string, 0)
	for _, chg := range snapAutoRefreshChanges {
		chgIds = append(chgIds, chg.ID())
		var updatedSnaps []string
		mylog.Check(chg.Get("snap-names", &updatedSnaps))

		snapNames = append(snapNames, updatedSnaps...)
	}

	// we have changes that the client should wait for before being ready
	return SyncResponse(&consoleConfStartRoutineResult{
		ActiveAutoRefreshChanges: chgIds,
		ActiveAutoRefreshSnaps:   snapNames,
	})
}
