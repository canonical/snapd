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
	scheduleConf, _, _ := getRefreshScheduleConf(r.state)
	refreshManaged := scheduleConf == "managed" && CanManageRefreshes(r.state)

	var err error
	perfTimings := timings.New(map[string]string{"ensure": "refresh-hints"})
	defer perfTimings.Save(r.state)

	var updates []*snap.Info
	var ignoreValidationByInstanceName map[string]bool
	timings.Run(perfTimings, "refresh-candidates", "query store for refresh candidates", func(tm timings.Measurer) {
		updates, _, ignoreValidationByInstanceName, err = refreshCandidates(auth.EnsureContextTODO(), r.state, nil, nil, nil, &store.RefreshOptions{RefreshManaged: refreshManaged})
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
		return fmt.Errorf("internal error: cannot get refresh-candidates: %v", err)
	}

	setNewRefreshCandidates(r.state, hints)
	return nil
}

// AtSeed configures hints refresh policies at end of seeding.
func (r *refreshHints) AtSeed() error {
	// on classic hold hints refreshes for a full 24h
	if release.OnClassic {
		var t1 time.Time
		err := r.state.Get("last-refresh-hints", &t1)
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

	online, err := isStoreOnline(r.state)
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

func refreshHintsFromCandidates(st *state.State, updates []*snap.Info, ignoreValidationByInstanceName map[string]bool, deviceCtx DeviceContext) (map[string]*refreshCandidate, error) {
	if ValidateRefreshes != nil && len(updates) != 0 {
		userID := 0
		var err error
		updates, err = ValidateRefreshes(st, updates, ignoreValidationByInstanceName, userID, deviceCtx)
		if err != nil {
			return nil, err
		}
	}

	hints := make(map[string]*refreshCandidate, len(updates))
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

		monitoring := isSnapMonitored(st, update.InstanceName())
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
	gateAutoRefreshHook, err := features.Flag(tr, features.GateAutoRefreshHook)
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
		err = st.Get("refresh-candidates", &rc)
		if err != nil {
			if errors.Is(err, state.ErrNoState) {
				// nothing to do
				return nil
			}
		}
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

	err = st.Get("refresh-candidates", &candidates)
	if err != nil {
		if errors.Is(err, state.ErrNoState) {
			return nil
		}
		return err
	}

	for _, snapName := range snaps {
		delete(candidates, snapName)
	}

	setNewRefreshCandidates(st, candidates)
	return nil
}

// updateRefreshCandidates updates the current set of refresh candidates stored
// in the state. When the list of canDropOldNames is empty, existing entries
// which aren't part of the update are dropped. When the list if non empty, only
// those entries mentioned in the list are dropped, other existing entries are
// preserved. Whenever an existing entry is to be updated, only its snap-setup
// content is changed, other fields remain unchanged.
func updateRefreshCandidates(st *state.State, hints map[string]*refreshCandidate, canDropOldNames []string) error {
	var oldHints map[string]*refreshCandidate
	if err := st.Get("refresh-candidates", &oldHints); err != nil {
		if !errors.Is(err, &state.NoStateError{}) {
			return err
		}
	}

	if len(oldHints) == 0 {
		st.Set("refresh-candidates", hints)
		return nil
	}

	dropSelectOld := len(canDropOldNames) != 0

	var deleted []string

	// selectively process existing entries
	for oldHintName, oldHint := range oldHints {
		newHint, hasUpdate := hints[oldHintName]

		if hasUpdate {
			// this hint has an update, but we only override snap
			// setup
			oldHint.SnapSetup = newHint.SnapSetup
		} else {
			if !dropSelectOld || (dropSelectOld && strutil.ListContains(canDropOldNames, oldHintName)) {
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

// setNewRefreshCandidates is used to set/replace "refresh-candidates" making
// sure that any snap that is no longer a candidate has its monitoring stopped.
// Must always be used when replacing the full "refresh-candidates"
func setNewRefreshCandidates(st *state.State, hints map[string]*refreshCandidate) {
	stopMonitoringOutdatedCandidates(st, hints)
	if len(hints) == 0 {
		st.Set("refresh-candidates", nil)
		return
	}
	st.Set("refresh-candidates", hints)
}

// stopMonitoringOutdatedCandidates aborts the monitoring for snaps for which a
// refresh candidate has been removed (possibly because the channel was reverted
// to an older version)
func stopMonitoringOutdatedCandidates(st *state.State, hints map[string]*refreshCandidate) {
	var oldHints map[string]*refreshCandidate
	if err := st.Get("refresh-candidates", &oldHints); err != nil {
		if errors.Is(err, &state.NoStateError{}) {
			// nothing to abort
			return
		}

		logger.Noticef("cannot abort removed refresh candidates: %v", err)
		return
	}

	for oldCand := range oldHints {
		if _, ok := hints[oldCand]; !ok {
			abortMonitoring(st, oldCand)
		}
	}
}
