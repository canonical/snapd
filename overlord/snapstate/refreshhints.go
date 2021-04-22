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

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/timings"
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

func (r *refreshHints) lastRefresh(timestampKey string) (time.Time, error) {
	return getTime(r.state, timestampKey)
}

func (r *refreshHints) needsUpdate() (bool, error) {
	tFull, err := r.lastRefresh("last-refresh")
	if err != nil {
		return false, err
	}
	tHints, err := r.lastRefresh("last-refresh-hints")
	if err != nil {
		return false, err
	}

	recentEnough := time.Now().Add(-refreshHintsDelay)
	if tFull.After(recentEnough) || tFull.Equal(recentEnough) {
		return false, nil
	}
	return tHints.Before(recentEnough), nil
}

func (r *refreshHints) refresh() error {
	var refreshManaged bool
	refreshManaged, _, _ = refreshScheduleManaged(r.state)

	var err error
	perfTimings := timings.New(map[string]string{"ensure": "refresh-hints"})
	defer perfTimings.Save(r.state)

	var updates []*snap.Info
	var ignoreValidationByInstanceName map[string]bool
	timings.Run(perfTimings, "refresh-candidates", "query store for refresh candidates", func(tm timings.Measurer) {
		updates, _, ignoreValidationByInstanceName, err = refreshCandidates(auth.EnsureContextTODO(), r.state, nil, nil, &store.RefreshOptions{RefreshManaged: refreshManaged})
	})
	// TODO: we currently set last-refresh-hints even when there was an
	// error. In the future we may retry with a backoff.
	r.state.Set("last-refresh-hints", time.Now())

	if err != nil {
		return err
	}
	deviceCtx, err := DeviceCtxFromState(r.state, nil)
	if err != nil {
		return err
	}
	hints, err := refreshHintsFromCandidates(r.state, updates, ignoreValidationByInstanceName, deviceCtx)
	if err != nil {
		return err
	}
	r.state.Set("refresh-candidates", hints)
	return nil
}

// AtSeed configures hints refresh policies at end of seeding.
func (r *refreshHints) AtSeed() error {
	// on classic hold hints refreshes for a full 24h
	if release.OnClassic {
		var t1 time.Time
		err := r.state.Get("last-refresh-hints", &t1)
		if err != state.ErrNoState {
			// already set or other error
			return err
		}
		r.state.Set("last-refresh-hints", time.Now())
	}
	return nil
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

func refreshHintsFromCandidates(st *state.State, updates []*snap.Info, ignoreValidationByInstanceName map[string]bool, deviceCtx DeviceContext) ([]*refreshCandidate, error) {
	if ValidateRefreshes != nil && len(updates) != 0 {
		userID := 0
		var err error
		updates, err = ValidateRefreshes(st, updates, ignoreValidationByInstanceName, userID, deviceCtx)
		if err != nil {
			return nil, err
		}
	}

	hints := []*refreshCandidate{}
	for _, update := range updates {
		var snapst SnapState
		if err := Get(st, update.InstanceName(), &snapst); err != nil {
			return nil, err
		}

		flags := snapst.Flags
		flags.IsAutoRefresh = true
		flags, err := earlyChecks(st, &snapst, update, flags)
		if err != nil {
			logger.Debugf("update hint for %q is not applicable: %v", update.InstanceName(), err)
			continue
		}

		snapsup := &refreshCandidate{
			SnapSetup{
				Base:      update.Base,
				Prereq:    defaultContentPlugProviders(st, update),
				Channel:   snapst.TrackingChannel,
				CohortKey: snapst.CohortKey,
				// UserID not set
				Flags:        flags.ForSnapSetup(),
				DownloadInfo: &update.DownloadInfo,
				SideInfo:     &update.SideInfo,
				Type:         update.Type(),
				PlugsOnly:    len(update.Slots) == 0,
				InstanceKey:  update.InstanceKey,
				auxStoreInfo: auxStoreInfo{
					Website: update.Website,
					Media:   update.Media,
				},
			}}
		hints = append(hints, snapsup)
	}
	return hints, nil
}
