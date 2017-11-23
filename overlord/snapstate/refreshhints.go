// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"time"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/store"
)

var refreshHintsDelay = time.Duration(24 * time.Hour)

// refreshHints will ensure that we get regular data about refreshes
// so that we can potentially warn the user about important missing
// refreshes.
type refreshHints struct {
	state *state.State
}

func newRefreshHints(st *state.State) *refreshHints {
	return &refreshHints{state: st}
}

func (r *refreshHints) lastRefresh() (time.Time, error) {
	var lastRefresh time.Time
	if err := r.state.Get("last-refresh-hints", &lastRefresh); err != nil && err != state.ErrNoState {
		return time.Time{}, err
	}
	return lastRefresh, nil
}

func (r *refreshHints) needsUpdate() (bool, error) {
	t, err := r.lastRefresh()
	if err != nil {
		return false, err
	}
	return t.Before(time.Now().Add(-refreshHintsDelay)), nil
}

func (r *refreshHints) refresh() error {
	var refreshManaged bool
	if RefreshScheduleManaged != nil {
		refreshManaged = RefreshScheduleManaged(r.state)
	}

	_, _, _, err := refreshCandidates(r.state, nil, nil, &store.RefreshOptions{RefreshManaged: refreshManaged})
	// TODO: we currently set last-refresh-hints even when there was an
	// error. In the future we may retry with a backoff.
	r.state.Set("last-refresh-hints", time.Now())
	return err
}

// Ensure will ensure that refresh hints are available on a regular
// interval.
func (r *refreshHints) Ensure() error {
	r.state.Lock()
	defer r.state.Unlock()

	// CanAutoRefresh is a hook that is set by the devicestate
	// code to ensure that we only AutoRefersh if the device has
	// bootstraped itself enough. This is only nil when snapstate
	// is used in isolation (like in tests).
	if CanAutoRefresh == nil {
		return nil
	}
	if ok, err := CanAutoRefresh(r.state); err != nil || !ok {
		return err
	}

	needsUpdate, err := r.needsUpdate()
	if err != nil {
		return err
	}
	if !needsUpdate {
		return nil
	}
	return r.refresh()
}
