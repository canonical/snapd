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
	_, _, _, err := refreshCandidates(r.state, nil, nil, &store.RefreshOptions{RefreshManaged: true})
	r.state.Set("last-refresh-hints", time.Now())
	return err
}

func (r *refreshHints) Ensure() error {
	needsUpdate, err := r.needsUpdate()
	if err != nil {
		return err
	}
	if !needsUpdate {
		return nil
	}
	return r.refresh()
}
