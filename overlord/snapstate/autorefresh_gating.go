// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021-2022 Canonical Ltd
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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

var gateAutoRefreshHookName = "gate-auto-refresh"

// gateAutoRefreshAction represents the action executed by
// snapctl refresh --hold or --proceed and stored in the context of
// gate-auto-refresh hook.
type GateAutoRefreshAction int

const (
	GateAutoRefreshProceed GateAutoRefreshAction = iota
	GateAutoRefreshHold
)

// cumulative hold time for snaps other than self
const maxOtherHoldDuration = time.Hour * 48

var timeNow = func() time.Time {
	return time.Now()
}

func lastRefreshed(st *state.State, snapName string) (time.Time, error) {
	var snapst SnapState
	if err := Get(st, snapName, &snapst); err != nil {
		return time.Time{}, fmt.Errorf("internal error, cannot get snap %q: %v", snapName, err)
	}
	// try to get last refresh time from snapstate, but it may not be present
	// for snaps installed before the introduction of last-refresh attribute.
	if snapst.LastRefreshTime != nil {
		return *snapst.LastRefreshTime, nil
	}
	snapInfo, err := snapst.CurrentInfo()
	if err != nil {
		return time.Time{}, err
	}
	// fall back to the modification time of .snap blob file as it's the best
	// approximation of last refresh time.
	fst, err := os.Stat(snapInfo.MountFile())
	if err != nil {
		return time.Time{}, err
	}
	return fst.ModTime(), nil
}

// HoldLevel determines which refresh operations are controlled by the hold.
// Levels are ordered and higher levels imply lower ones.
type HoldLevel int

const (
	// HoldAutoRefresh holds snaps only in auto-refresh operations
	HoldAutoRefresh HoldLevel = iota
	// HoldGeneral holds snaps in general and auto-refresh operations
	HoldGeneral
)

type holdState struct {
	// FirstHeld keeps the time when the given snap was first held for refresh by a gating snap.
	FirstHeld time.Time `json:"first-held"`
	// HoldUntil stores the desired end time for holding.
	HoldUntil time.Time `json:"hold-until"`
	// Level of this hold.
	Level HoldLevel `json:"level,omitempty"`
}

func refreshGating(st *state.State) (map[string]map[string]*holdState, error) {
	// held snaps -> holding snap(s) -> first-held/hold-until time
	var gating map[string]map[string]*holdState
	err := st.Get("snaps-hold", &gating)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, fmt.Errorf("internal error: cannot get snaps-hold: %v", err)
	}
	if errors.Is(err, state.ErrNoState) {
		return make(map[string]map[string]*holdState), nil
	}
	return gating, nil
}

// HoldDurationError contains the that error prevents requested hold, along with
// hold time that's left (if any).
type HoldDurationError struct {
	Err          error
	DurationLeft time.Duration
}

func (h *HoldDurationError) Error() string {
	return h.Err.Error()
}

// HoldError contains the details of snaps that cannot to be held.
type HoldError struct {
	SnapsInError map[string]HoldDurationError
}

func (h *HoldError) Error() string {
	l := []string{""}
	for _, e := range h.SnapsInError {
		l = append(l, e.Error())
	}
	return fmt.Sprintf("cannot hold some snaps:%s", strings.Join(l, "\n - "))
}

func maxAllowedPostponement(gatingSnap, affectedSnap string, maxPostponement time.Duration) time.Duration {
	if affectedSnap == gatingSnap {
		return maxPostponement
	}
	return maxOtherHoldDuration
}

// holdDurationLeft computes the maximum duration that's left for holding a refresh
// given current time, last refresh time, time when snap was first held, maximum
// duration allowed for the given snap and maximum overall postponement allowed by
// snapd.
func holdDurationLeft(now time.Time, lastRefresh, firstHeld time.Time, maxDuration, maxPostponement time.Duration) time.Duration {
	d1 := firstHeld.Add(maxDuration).Sub(now)
	d2 := lastRefresh.Add(maxPostponement).Sub(now)
	if d1 < d2 {
		return d1
	}
	return d2
}

// HoldRefreshesBySystem is used to hold snaps by the sys admin (denoted by the
// "system" holding snap). HoldTime can be "forever" to denote an indefinite hold
// or any RFC3339 timestamp.
// A hold level can be specified indicating which operations are affected by the
// hold.
func HoldRefreshesBySystem(st *state.State, level HoldLevel, holdTime string, holdSnaps []string) error {
	snaps, err := All(st)
	if err != nil {
		return err
	}

	for _, holdSnap := range holdSnaps {
		if _, ok := snaps[holdSnap]; !ok {
			return snap.NotInstalledError{Snap: holdSnap}
		}
	}

	// zero value durations denote max allowed time in HoldRefresh
	var holdDuration time.Duration
	if holdTime != "forever" {
		holdTime, err := time.Parse(time.RFC3339, holdTime)
		if err != nil {
			return err
		}

		holdDuration = holdTime.Sub(timeNow())
	}

	_, err = HoldRefresh(st, level, "system", holdDuration, holdSnaps...)
	return err
}

// HoldRefresh marks affectingSnaps as held for refresh for up to holdTime.
// HoldTime of zero denotes maximum allowed hold time.
// Holding fails if not all snaps can be held, in that case HoldError is returned
// and it contains the details of snaps that prevented holding. On success the
// function returns the remaining hold time. The remaining hold time is the
// minimum of the remaining hold time for all affecting snaps.
// A hold level can be specified indicating which operations are affected by the
// hold.
func HoldRefresh(st *state.State, level HoldLevel, gatingSnap string, holdDuration time.Duration, affectingSnaps ...string) (time.Duration, error) {
	gating, err := refreshGating(st)
	if err != nil {
		return 0, err
	}
	herr := &HoldError{
		SnapsInError: make(map[string]HoldDurationError),
	}

	var durationMin time.Duration

	now := timeNow()
	for _, heldSnap := range affectingSnaps {
		var left time.Duration

		hold, ok := gating[heldSnap][gatingSnap]
		if !ok {
			hold = &holdState{
				FirstHeld: now,
			}
		}
		hold.Level = level

		if gatingSnap == "system" {
			if holdDuration == 0 {
				holdDuration = maxDuration
			}

			// if snap is being gated by "system" (it was set by the system admin), it
			// can be held by any amount of time and no checks are required
			hold.HoldUntil = now.Add(holdDuration)
			left = holdDuration
		} else {
			lastRefreshTime, err := lastRefreshed(st, heldSnap)
			if err != nil {
				return 0, err
			}

			mp := maxPostponement - maxPostponementBuffer
			maxDur := maxAllowedPostponement(gatingSnap, heldSnap, mp)

			// calculate max hold duration that's left considering previous hold
			// requests of this snap and last refresh time.
			left = holdDurationLeft(now, lastRefreshTime, hold.FirstHeld, maxDur, mp)
			if left <= 0 {
				herr.SnapsInError[heldSnap] = HoldDurationError{
					Err: fmt.Errorf("snap %q cannot hold snap %q anymore, maximum refresh postponement exceeded", gatingSnap, heldSnap),
				}
				continue
			}

			dur := holdDuration
			if dur == 0 {
				// duration not specified, using a default one (maximum) or what's
				// left of it.
				dur = left
			} else {
				// explicit hold duration requested
				if dur > maxDur {
					herr.SnapsInError[heldSnap] = HoldDurationError{
						Err:          fmt.Errorf("requested holding duration for snap %q of %s by snap %q exceeds maximum holding time", heldSnap, holdDuration, gatingSnap),
						DurationLeft: left,
					}
					continue
				}
			}

			newHold := now.Add(dur)
			cutOff := lastRefreshTime.Add(maxPostponement - maxPostponementBuffer)

			// consider last refresh time and adjust hold duration if needed so it's
			// not exceeded.
			if newHold.Before(cutOff) {
				hold.HoldUntil = newHold
			} else {
				hold.HoldUntil = cutOff
			}
		}

		// finally store/update gating hold data
		if _, ok := gating[heldSnap]; !ok {
			gating[heldSnap] = make(map[string]*holdState)
		}
		gating[heldSnap][gatingSnap] = hold

		// note, left is guaranteed to be > 0 at this point
		if durationMin == 0 || left < durationMin {
			durationMin = left
		}
	}

	if len(herr.SnapsInError) > 0 {
		// if some of the affecting snaps couldn't be held anymore then it
		// doesn't make sense to hold other affecting snaps (because the gating
		// snap is going to be disrupted anyway); go over all affectingSnaps
		// again and remove gating info for them - this also deletes old holdings
		// (if the hook run on previous refresh attempt) therefore we need to
		// update snaps-hold state below.
		for _, heldSnap := range affectingSnaps {
			delete(gating[heldSnap], gatingSnap)
		}
	}
	st.Set("snaps-hold", gating)
	if len(herr.SnapsInError) > 0 {
		return 0, herr
	}
	return durationMin, nil
}

// ProceedWithRefresh unblocks a set of snaps held by gatingSnap for refresh.
// If no snaps are specified, all snaps held by gatingSnap are unblocked. This
// should be called for --proceed on the gatingSnap.
func ProceedWithRefresh(st *state.State, gatingSnap string, unholdSnaps []string) error {
	gating, err := refreshGating(st)
	if err != nil {
		return err
	}

	var changed bool
	for heldSnap, gatingSnaps := range gating {
		if len(unholdSnaps) != 0 && !strutil.ListContains(unholdSnaps, heldSnap) {
			continue
		}

		if _, ok := gatingSnaps[gatingSnap]; ok {
			delete(gatingSnaps, gatingSnap)
			changed = true
		}
		if len(gatingSnaps) == 0 {
			delete(gating, heldSnap)
		}
	}

	if changed {
		st.Set("snaps-hold", gating)
	}

	return nil
}

// pruneGating removes affecting snaps that are not in candidates (meaning
// there is no update for them anymore).
func pruneGating(st *state.State, candidates map[string]*refreshCandidate) error {
	gating, err := refreshGating(st)
	if err != nil {
		return err
	}

	if len(gating) == 0 {
		return nil
	}

	var changed bool
	for affectingSnap := range gating {
		if candidates[affectingSnap] == nil {
			// the snap doesn't have an update anymore, forget it
			// unless there is a user/system hold
			changed = pruneHoldStatesForSnap(gating, affectingSnap)
		}
	}
	if changed {
		st.Set("snaps-hold", gating)
	}
	return nil
}

// pruneHoldStatesForSnap prunes hold state for the snap for any holding
// by another snaps, but preserve user/system holding.
func pruneHoldStatesForSnap(gating map[string]map[string]*holdState, snapName string) (changed bool) {
	holdingSnaps := gating[snapName]
	for holdingSnap := range holdingSnaps {
		if holdingSnap == "system" {
			continue
		}
		delete(holdingSnaps, holdingSnap)
		changed = true
	}
	if len(holdingSnaps) == 0 {
		delete(gating, snapName)
		changed = true
	}
	return changed
}

// resetGatingForRefreshed resets gating information by removing refreshedSnaps
// (they are not held anymore). This should be called for snaps about to be
// refreshed.
func resetGatingForRefreshed(st *state.State, refreshedSnaps ...string) error {
	gating, err := refreshGating(st)
	if err != nil {
		return err
	}
	if len(gating) == 0 {
		return nil
	}

	var changed bool
	for _, snapName := range refreshedSnaps {
		if _, ok := gating[snapName]; ok {
			// holds placed by the user remain after a refresh
			changed = pruneHoldStatesForSnap(gating, snapName)
		}
	}

	if changed {
		st.Set("snaps-hold", gating)
	}
	return nil
}

// pruneSnapsHold removes the given snap from snaps-hold, whether it was an
// affecting snap or gating snap. This should be called when a snap gets
// removed.
func pruneSnapsHold(st *state.State, snapName string) error {
	gating, err := refreshGating(st)
	if err != nil {
		return err
	}
	if len(gating) == 0 {
		return nil
	}

	var changed bool

	if _, ok := gating[snapName]; ok {
		delete(gating, snapName)
		changed = true
	}

	for heldSnap, holdingSnaps := range gating {
		if _, ok := holdingSnaps[snapName]; ok {
			delete(holdingSnaps, snapName)
			if len(holdingSnaps) == 0 {
				delete(gating, heldSnap)
			}
			changed = true
		}
	}

	if changed {
		st.Set("snaps-hold", gating)
	}

	return nil
}

// HeldSnaps returns all snaps that are held at the given level or at more
// restricting ones and shouldn't be refreshed. The snaps are mapped to a list
// of snaps with currently effective holds on them.
func HeldSnaps(st *state.State, level HoldLevel) (map[string][]string, error) {
	gating, err := refreshGating(st)
	if err != nil {
		return nil, err
	}
	if len(gating) == 0 {
		return nil, nil
	}

	now := timeNow()

	held := make(map[string][]string)
	for heldSnap, holds := range gating {
		lastRefresh, err := lastRefreshed(st, heldSnap)
		if err != nil {
			return nil, err
		}

		for holdingSnap, hold := range holds {
			// the snap is not considered held for the given
			// level (e.g HoldGeneral) but only for lower
			// levels (e.g. HoldAutorefresh)
			if hold.Level < level {
				continue
			}
			// enforce the maxPostponement limit on a hold, unless it's held by the user
			if holdingSnap != "system" && lastRefresh.Add(maxPostponement).Before(now) {
				continue
			}

			if hold.HoldUntil.Before(now) {
				continue
			}

			held[heldSnap] = append(held[heldSnap], holdingSnap)
		}
	}
	return held, nil
}

// SystemHold returns the time until which the snap's refreshes have been held
// by the sysadmin. If no such hold exists, returns a zero time.Time value.
func SystemHold(st *state.State, snap string) (time.Time, error) {
	gating, err := refreshGating(st)
	if err != nil {
		return time.Time{}, err
	}

	holds := gating[snap]
	for holdingSnap, hold := range holds {
		if holdingSnap == "system" {
			return hold.HoldUntil, nil
		}
	}

	return time.Time{}, nil
}

// LongestGatingHold returns the time until which the snap's refreshes have been held
// by a gating snap. If no such hold exists, returns a zero time.Time value.
func LongestGatingHold(st *state.State, snap string) (time.Time, error) {
	gating, err := refreshGating(st)
	if err != nil {
		return time.Time{}, err
	}

	holds := gating[snap]

	var lastHold time.Time
	for holdingSnap, timeRange := range holds {
		if holdingSnap != "system" && timeRange.HoldUntil.After(lastHold) {
			lastHold = timeRange.HoldUntil
		}
	}

	return lastHold, nil
}

type AffectedSnapInfo struct {
	Restart        bool
	Base           bool
	AffectingSnaps map[string]bool
}

// AffectedByRefreshCandidates returns information about all snaps affected by
// current refresh-candidates in the state.
func AffectedByRefreshCandidates(st *state.State) (map[string]*AffectedSnapInfo, error) {
	// we care only about the keys so this can use
	// *json.RawMessage instead of refreshCandidates
	var candidates map[string]*json.RawMessage
	if err := st.Get("refresh-candidates", &candidates); err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}

	snaps := make([]string, 0, len(candidates))
	for cand := range candidates {
		snaps = append(snaps, cand)
	}
	affected, err := affectedByRefresh(st, snaps)
	return affected, err
}

// AffectingSnapsForAffectedByRefreshCandidates returns the list of all snaps
// affecting affectedSnap (i.e. a gating snap), based on upcoming updates
// from refresh-candidates.
func AffectingSnapsForAffectedByRefreshCandidates(st *state.State, affectedSnap string) ([]string, error) {
	affected, err := AffectedByRefreshCandidates(st)
	if err != nil {
		return nil, err
	}
	affectedInfo := affected[affectedSnap]
	if affectedInfo == nil || len(affectedInfo.AffectingSnaps) == 0 {
		return nil, nil
	}
	affecting := make([]string, 0, len(affectedInfo.AffectingSnaps))
	for sn := range affectedInfo.AffectingSnaps {
		affecting = append(affecting, sn)
	}
	sort.Strings(affecting)
	return affecting, nil
}

func affectedByRefresh(st *state.State, updates []string) (map[string]*AffectedSnapInfo, error) {
	allSnaps, err := All(st)
	if err != nil {
		return nil, err
	}
	snapsWithHook := make(map[string]*SnapState)

	var bootBase string
	if !release.OnClassic {
		deviceCtx, err := DeviceCtx(st, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("cannot get device context: %v", err)
		}
		bootBaseInfo, err := BootBaseInfo(st, deviceCtx)
		if err != nil {
			return nil, fmt.Errorf("cannot get boot base info: %v", err)
		}
		bootBase = bootBaseInfo.InstanceName()
	}

	byBase := make(map[string][]string)
	for name, snapSt := range allSnaps {
		if !snapSt.Active {
			delete(allSnaps, name)
			continue
		}
		inf, err := snapSt.CurrentInfo()
		if err != nil {
			return nil, err
		}
		// optimization: do not consider snaps that don't have gate-auto-refresh hook.
		if inf.Hooks[gateAutoRefreshHookName] == nil {
			continue
		}
		snapsWithHook[name] = snapSt

		base := inf.Base
		if base == "none" {
			continue
		}
		if inf.Base == "" {
			base = "core"
		}
		byBase[base] = append(byBase[base], snapSt.InstanceName())
	}

	affected := make(map[string]*AffectedSnapInfo)

	addAffected := func(snapName, affectedBy string, restart bool, base bool) {
		if affected[snapName] == nil {
			affected[snapName] = &AffectedSnapInfo{
				AffectingSnaps: map[string]bool{},
			}
		}
		affectedInfo := affected[snapName]
		if restart {
			affectedInfo.Restart = restart
		}
		if base {
			affectedInfo.Base = base
		}
		affectedInfo.AffectingSnaps[affectedBy] = true
	}

	for _, snapName := range updates {
		snapSt := allSnaps[snapName]
		if snapSt == nil {
			// this could happen if an update for inactive snap was requested (those
			// are filtered out above).
			return nil, fmt.Errorf("internal error: no state for snap %q", snapName)
		}
		up, err := snapSt.CurrentInfo()
		if err != nil {
			return nil, err
		}

		// the snap affects itself (as long as it has the hook)
		if snapSt := snapsWithHook[up.InstanceName()]; snapSt != nil {
			addAffected(up.InstanceName(), up.InstanceName(), false, false)
		}

		// on core system, affected by update of boot base
		if bootBase != "" && up.InstanceName() == bootBase {
			for _, snapSt := range snapsWithHook {
				addAffected(snapSt.InstanceName(), up.InstanceName(), true, false)
			}
		}

		// snaps that can trigger reboot
		// XXX: gadget refresh doesn't always require reboot, refine this
		if up.Type() == snap.TypeKernel || up.Type() == snap.TypeGadget {
			for _, snapSt := range snapsWithHook {
				addAffected(snapSt.InstanceName(), up.InstanceName(), true, false)
			}
			continue
		}
		if up.Type() == snap.TypeBase || up.SnapName() == "core" {
			// affected by refresh of this base snap
			for _, snapName := range byBase[up.InstanceName()] {
				addAffected(snapName, up.InstanceName(), false, true)
			}
		}

		repo := ifacerepo.Get(st)

		// consider slots provided by refreshed snap, but exclude core and snapd
		// since they provide system-level slots that are generally not disrupted
		// by snap updates.
		if up.SnapType != snap.TypeSnapd && up.SnapName() != "core" {
			for _, slotInfo := range up.Slots {
				conns, err := repo.Connected(up.InstanceName(), slotInfo.Name)
				if err != nil {
					return nil, err
				}
				for _, cref := range conns {
					// affected only if it wasn't optimized out above
					if snapsWithHook[cref.PlugRef.Snap] != nil {
						addAffected(cref.PlugRef.Snap, up.InstanceName(), true, false)
					}
				}
			}
		}

		// consider plugs/slots with AffectsPlugOnRefresh flag;
		// for slot side only consider snapd/core because they are ignored by the
		// earlier loop around slots.
		if up.SnapType == snap.TypeSnapd || up.SnapType == snap.TypeOS {
			for _, slotInfo := range up.Slots {
				iface := repo.Interface(slotInfo.Interface)
				if iface == nil {
					return nil, fmt.Errorf("internal error: unknown interface %s", slotInfo.Interface)
				}
				si := interfaces.StaticInfoOf(iface)
				if !si.AffectsPlugOnRefresh {
					continue
				}
				conns, err := repo.Connected(up.InstanceName(), slotInfo.Name)
				if err != nil {
					return nil, err
				}
				for _, cref := range conns {
					if snapsWithHook[cref.PlugRef.Snap] != nil {
						addAffected(cref.PlugRef.Snap, up.InstanceName(), true, false)
					}
				}
			}
		}
	}

	return affected, nil
}

// createGateAutoRefreshHooks creates gate-auto-refresh hooks for all affectedSnaps.
// Hook tasks will be chained to run sequentially.
func createGateAutoRefreshHooks(st *state.State, affectedSnaps []string) *state.TaskSet {
	ts := state.NewTaskSet()
	var prev *state.Task
	for _, snapName := range affectedSnaps {
		hookTask := SetupGateAutoRefreshHook(st, snapName)
		// XXX: it should be fine to run the hooks in parallel
		if prev != nil {
			hookTask.WaitFor(prev)
		}
		ts.AddTask(hookTask)
		prev = hookTask
	}
	return ts
}

func conditionalAutoRefreshAffectedSnaps(t *state.Task) ([]string, error) {
	var snaps map[string]*json.RawMessage
	if err := t.Get("snaps", &snaps); err != nil {
		return nil, fmt.Errorf("internal error: cannot get snaps to update for %s task %s", t.Kind(), t.ID())
	}
	names := make([]string, 0, len(snaps))
	for sn := range snaps {
		// TODO: drop snaps once we know the outcome of gate-auto-refresh hooks.
		names = append(names, sn)
	}
	return names, nil
}

// snapsToRefresh returns all snaps that should proceed with refresh considering
// hold behavior.
var snapsToRefresh = func(gatingTask *state.Task) ([]*refreshCandidate, error) {
	var snaps map[string]*refreshCandidate
	if err := gatingTask.Get("snaps", &snaps); err != nil {
		return nil, err
	}

	held, err := HeldSnaps(gatingTask.State(), HoldAutoRefresh)
	if err != nil {
		return nil, err
	}

	var skipped []string
	var candidates []*refreshCandidate
	for _, s := range snaps {
		if _, ok := held[s.InstanceName()]; !ok {
			candidates = append(candidates, s)
		} else {
			skipped = append(skipped, s.InstanceName())
		}
	}

	if len(skipped) > 0 {
		sort.Strings(skipped)
		logger.Noticef("skipping refresh of held snaps: %s", strings.Join(skipped, ","))
	}

	return candidates, nil
}

// AutoRefreshForGatingSnap triggers an auto-refresh change for all
// snaps held by the given gating snap. This should only be called if the
// gate-auto-refresh-hook feature is enabled.
// TODO: this should be restricted as it doesn't take refresh timer/refresh hold
// into account.
func AutoRefreshForGatingSnap(st *state.State, gatingSnap string) error {
	// ensure nothing is in flight already
	if autoRefreshInFlight(st) {
		return fmt.Errorf("there is an auto-refresh in progress")
	}

	gating, err := refreshGating(st)
	if err != nil {
		return err
	}

	var hasHeld bool
	for _, holdingSnaps := range gating {
		if _, ok := holdingSnaps[gatingSnap]; ok {
			hasHeld = true
			break
		}
	}
	if !hasHeld {
		return fmt.Errorf("no snaps are held by snap %q", gatingSnap)
	}

	// NOTE: this will unlock and re-lock state for network ops
	// XXX: should we refresh assertions (just call AutoRefresh()?)
	updated, tasksets, err := autoRefreshPhase1(auth.EnsureContextTODO(), st, gatingSnap)
	if err != nil {
		return err
	}
	msg := autoRefreshSummary(updated)
	if msg == "" {
		logger.Noticef("auto-refresh: all snaps previously held by %q are up-to-date", gatingSnap)
		return nil
	}

	// note, we do not update last-refresh timestamp because this auto-refresh
	// is not treated as a full auto-refresh.

	chg := st.NewChange("auto-refresh", msg)
	for _, ts := range tasksets {
		chg.AddAll(ts)
	}
	chg.Set("snap-names", updated)
	chg.Set("api-data", map[string]interface{}{"snap-names": updated})

	return nil
}
