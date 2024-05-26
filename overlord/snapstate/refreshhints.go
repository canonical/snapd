// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2023 Canonical Ltd
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
	"fmt"
	"reflect"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil"
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
	tFull := mylog.Check2(r.lastRefresh("last-refresh"))

	tHints := mylog.Check2(r.lastRefresh("last-refresh-hints"))

	recentEnough := time.Now().Add(-refreshHintsDelay)
	if tFull.After(recentEnough) || tFull.Equal(recentEnough) {
		return false, nil
	}
	return tHints.Before(recentEnough), nil
}

func (r *refreshHints) refresh() error {
	scheduleConf, _, _ := getRefreshScheduleConf(r.state)
	refreshManaged := scheduleConf == "managed" && CanManageRefreshes(r.state)

	perfTimings := timings.New(map[string]string{"ensure": "refresh-hints"})
	defer perfTimings.Save(r.state)

	var updates []*snap.Info
	var ignoreValidationByInstanceName map[string]bool
	timings.Run(perfTimings, "refresh-candidates", "query store for refresh candidates", func(tm timings.Measurer) {
		updates, _, ignoreValidationByInstanceName = mylog.Check4(refreshCandidates(auth.EnsureContextTODO(),
			r.state, nil, nil, nil, &store.RefreshOptions{RefreshManaged: refreshManaged}))
	})
	// TODO: we currently set last-refresh-hints even when there was an
	// error. In the future we may retry with a backoff.
	r.state.Set("last-refresh-hints", time.Now())

	deviceCtx := mylog.Check2(DeviceCtxFromState(r.state, nil))

	hints := mylog.Check2(refreshHintsFromCandidates(r.state, updates, ignoreValidationByInstanceName, deviceCtx))

	// update candidates in state dropping all entries which are not part of
	// the new hints
	updateRefreshCandidates(r.state, hints, nil)
	return nil
}

// AtSeed configures hints refresh policies at end of seeding.
func (r *refreshHints) AtSeed() error {
	// on classic hold hints refreshes for a full 24h
	if release.OnClassic {
		var t1 time.Time
		mylog.Check(r.state.Get("last-refresh-hints", &t1))
		if !errors.Is(err, state.ErrNoState) {
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

	online := mylog.Check2(isStoreOnline(r.state))
	if err != nil || !online {
		return err
	}

	// CanAutoRefresh is a hook that is set by the devicestate
	// code to ensure that we only AutoRefresh if the device has
	// bootstraped itself enough. This is only nil when snapstate
	// is used in isolation (like in tests).
	if CanAutoRefresh == nil {
		return nil
	}
	if ok := mylog.Check2(CanAutoRefresh(r.state)); err != nil || !ok {
		return err
	}

	needsUpdate := mylog.Check2(r.needsUpdate())

	if !needsUpdate {
		return nil
	}
	return r.refresh()
}

func refreshHintsFromCandidates(st *state.State, updates []*snap.Info, ignoreValidationByInstanceName map[string]bool, deviceCtx DeviceContext) (map[string]*refreshCandidate, error) {
	if ValidateRefreshes != nil && len(updates) != 0 {
		userID := 0

		updates = mylog.Check2(ValidateRefreshes(st, updates, ignoreValidationByInstanceName, userID, deviceCtx))

	}

	hints := make(map[string]*refreshCandidate, len(updates))
	for _, update := range updates {
		var snapst SnapState
		mylog.Check(Get(st, update.InstanceName(), &snapst))

		flags := snapst.Flags
		flags.IsAutoRefresh = true
		flags := mylog.Check2(earlyChecks(st, &snapst, update, flags))

		monitoring := IsSnapMonitored(st, update.InstanceName())
		providerContentAttrs := defaultProviderContentAttrs(st, update, nil)
		snapsup := &refreshCandidate{
			SnapSetup: SnapSetup{
				Base:               update.Base,
				Prereq:             getKeys(providerContentAttrs),
				PrereqContentAttrs: providerContentAttrs,
				Channel:            snapst.TrackingChannel,
				CohortKey:          snapst.CohortKey,
				// UserID not set
				Flags:        flags.ForSnapSetup(),
				DownloadInfo: &update.DownloadInfo,
				SideInfo:     &update.SideInfo,
				Type:         update.Type(),
				Version:      update.Version,
				PlugsOnly:    len(update.Slots) == 0,
				InstanceKey:  update.InstanceKey,
				auxStoreInfo: auxStoreInfo{
					Media: update.Media,
					// XXX we store this for the benefit of
					// old snapd
					Website: update.Website(),
				},
			},
			// preserve fields not related to snap-setup
			Monitored: monitoring,
		}
		hints[update.InstanceName()] = snapsup
	}
	return hints, nil
}

// pruneRefreshCandidates removes the given snaps from refresh-candidates map
// in the state.
func pruneRefreshCandidates(st *state.State, snaps ...string) error {
	tr := config.NewTransaction(st)
	gateAutoRefreshHook := mylog.Check2(features.Flag(tr, features.GateAutoRefreshHook))
	if err != nil && !config.IsNoOption(err) {
		return err
	}
	// Remove refresh-candidates from state if gate-auto-refresh-hook feature is
	// not enabled and it is not a map. This acts as a workaround for the case where a snapd from
	// edge was used and created refresh-candidates in the old format (an array)
	// with the feature enabled, but the feature was then disabled so the new
	// map format will never make it into the state.
	// When the feature is enabled then auto-refresh code will re-initialize
	// refresh-candidates in the correct format expected here.
	// See https://forum.snapcraft.io/t/cannot-r-emove-snap-json-cannot-unmarshal-array-into-go-value-of-type-map-string-snapstate-refreshcandidate/27276
	if !gateAutoRefreshHook {
		var rc interface{}
		mylog.Check(st.Get("refresh-candidates", &rc))

		// nothing to do

		v := reflect.ValueOf(rc)
		if !v.IsValid() {
			// nothing to do
			return nil
		}
		if v.Kind() != reflect.Map {
			// just remove
			st.Set("refresh-candidates", nil)
			return nil
		}
	}

	var candidates map[string]*refreshCandidate
	mylog.Check(st.Get("refresh-candidates", &candidates))

	for _, snapName := range snaps {
		delete(candidates, snapName)
		abortMonitoring(st, snapName)
	}

	if len(candidates) == 0 {
		st.Set("refresh-candidates", nil)
	} else {
		st.Set("refresh-candidates", candidates)
	}

	return nil
}

// updateRefreshCandidates updates the current set of refresh candidates stored
// in the state. When the list of canDropOldNames is empty, existing entries
// which aren't part of the update are dropped. When the list is non empty, only
// those entries mentioned in the list are dropped, other existing entries are
// preserved. Whenever an existing entry is to be replaced, the caller must have
// provided a hint which preserves the hint's state outside of snap-setup.
func updateRefreshCandidates(st *state.State, hints map[string]*refreshCandidate, canDropOldNames []string) error {
	var oldHints map[string]*refreshCandidate
	mylog.Check(st.Get("refresh-candidates", &oldHints))

	if len(oldHints) == 0 {
		st.Set("refresh-candidates", hints)
		return nil
	}

	dropAllOld := len(canDropOldNames) == 0

	var deleted []string

	// selectively process existing entries
	for oldHintName := range oldHints {
		if newHint, hasUpdate := hints[oldHintName]; hasUpdate {
			// XXX we rely on the new hint preserving the state
			oldHints[oldHintName] = newHint
		} else {
			if dropAllOld || strutil.ListContains(canDropOldNames, oldHintName) {
				// we have no new hint for this snap
				deleted = append(deleted, oldHintName)
				delete(oldHints, oldHintName)
			}
		}
	}
	// now add all new entries
	for newHintName, newHint := range hints {
		// preserved entries have already been processed
		if _, processed := oldHints[newHintName]; !processed {
			oldHints[newHintName] = newHint
		}
	}

	// stop monitoring candidates which were deleted
	for _, dropped := range deleted {
		abortMonitoring(st, dropped)
	}

	st.Set("refresh-candidates", oldHints)
	return nil
}
