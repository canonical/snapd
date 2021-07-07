// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
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

type holdState struct {
	// FirstHeld keeps the time when the given snap was first held for refresh by a gating snap.
	FirstHeld time.Time `json:"first-held"`
	// HoldUntil stores the desired end time for holding.
	HoldUntil time.Time `json:"hold-until"`
}

func refreshGating(st *state.State) (map[string]map[string]*holdState, error) {
	// held snaps -> holding snap(s) -> first-held/hold-until time
	var gating map[string]map[string]*holdState
	err := st.Get("snaps-hold", &gating)
	if err != nil && err != state.ErrNoState {
		return nil, fmt.Errorf("internal error: cannot get snaps-hold: %v", err)
	}
	if err == state.ErrNoState {
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

// HoldRefresh marks affectingSnaps as held for refresh for up to holdTime.
// HoldTime of zero denotes maximum allowed hold time.
// Holding may fail for only some snaps in which case HoldError is returned and
// it contains the details of failed ones.
func HoldRefresh(st *state.State, gatingSnap string, holdDuration time.Duration, affectingSnaps ...string) error {
	gating, err := refreshGating(st)
	if err != nil {
		return err
	}
	herr := &HoldError{
		SnapsInError: make(map[string]HoldDurationError),
	}
	now := timeNow()
	for _, heldSnap := range affectingSnaps {
		hold, ok := gating[heldSnap][gatingSnap]
		if !ok {
			hold = &holdState{
				FirstHeld: now,
			}
		}

		lastRefreshTime, err := lastRefreshed(st, heldSnap)
		if err != nil {
			return err
		}

		mp := maxPostponement - maxPostponementBuffer
		maxDur := maxAllowedPostponement(gatingSnap, heldSnap, mp)

		// calculate max hold duration that's left considering previous hold
		// requests of this snap and last refresh time.
		left := holdDurationLeft(now, lastRefreshTime, hold.FirstHeld, maxDur, mp)
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

		// finally store/update gating hold data
		if _, ok := gating[heldSnap]; !ok {
			gating[heldSnap] = make(map[string]*holdState)
		}
		gating[heldSnap][gatingSnap] = hold
	}

	if len(herr.SnapsInError) != len(affectingSnaps) {
		st.Set("snaps-hold", gating)
	}
	if len(herr.SnapsInError) > 0 {
		return herr
	}
	return nil
}

// ProceedWithRefresh unblocks all snaps held by gatingSnap for refresh. This
// should be called for --proceed on the gatingSnap.
func ProceedWithRefresh(st *state.State, gatingSnap string) error {
	gating, err := refreshGating(st)
	if err != nil {
		return err
	}
	if len(gating) == 0 {
		return nil
	}

	var changed bool
	for heldSnap, gatingSnaps := range gating {
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
			delete(gating, affectingSnap)
			changed = true
		}
	}
	if changed {
		st.Set("snaps-hold", gating)
	}
	return nil
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
			delete(gating, snapName)
			changed = true
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

// heldSnaps returns all snaps that are gated and shouldn't be refreshed.
func heldSnaps(st *state.State) (map[string]bool, error) {
	gating, err := refreshGating(st)
	if err != nil {
		return nil, err
	}
	if len(gating) == 0 {
		return nil, nil
	}

	now := timeNow()

	held := make(map[string]bool)
Loop:
	for heldSnap, holdingSnaps := range gating {
		refreshed, err := lastRefreshed(st, heldSnap)
		if err != nil {
			return nil, err
		}
		// make sure we don't hold any snap for more than maxPostponement
		if refreshed.Add(maxPostponement).Before(now) {
			continue
		}
		for _, hold := range holdingSnaps {
			if hold.HoldUntil.Before(now) {
				continue
			}
			held[heldSnap] = true
			continue Loop
		}
	}
	return held, nil
}

type affectedSnapInfo struct {
	Restart        bool
	Base           bool
	AffectingSnaps map[string]bool
}

func affectedByRefresh(st *state.State, updates []*snap.Info) (map[string]*affectedSnapInfo, error) {
	all, err := All(st)
	if err != nil {
		return nil, err
	}

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
	for name, snapSt := range all {
		if !snapSt.Active {
			delete(all, name)
			continue
		}
		inf, err := snapSt.CurrentInfo()
		if err != nil {
			return nil, err
		}
		// optimization: do not consider snaps that don't have gate-auto-refresh hook.
		if inf.Hooks[gateAutoRefreshHookName] == nil {
			delete(all, name)
			continue
		}

		base := inf.Base
		if base == "none" {
			continue
		}
		if inf.Base == "" {
			base = "core"
		}
		byBase[base] = append(byBase[base], inf.InstanceName())
	}

	affected := make(map[string]*affectedSnapInfo)

	addAffected := func(snapName, affectedBy string, restart bool, base bool) {
		if affected[snapName] == nil {
			affected[snapName] = &affectedSnapInfo{
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

	for _, up := range updates {
		// on core system, affected by update of boot base
		if bootBase != "" && up.InstanceName() == bootBase {
			for _, snapSt := range all {
				addAffected(snapSt.InstanceName(), up.InstanceName(), true, false)
			}
		}

		// snaps that can trigger reboot
		// XXX: gadget refresh doesn't always require reboot, refine this
		if up.Type() == snap.TypeKernel || up.Type() == snap.TypeGadget {
			for _, snapSt := range all {
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
					if all[cref.PlugRef.Snap] != nil {
						addAffected(cref.PlugRef.Snap, up.InstanceName(), true, false)
					}
				}
			}
		}

		// consider mount backend plugs/slots;
		// for slot side only consider snapd/core because they are ignored by the
		// earlier loop around slots.
		if up.SnapType == snap.TypeSnapd || up.SnapType == snap.TypeOS {
			for _, slotInfo := range up.Slots {
				iface := repo.Interface(slotInfo.Interface)
				if iface == nil {
					return nil, fmt.Errorf("internal error: unknown interface %s", slotInfo.Interface)
				}
				if !usesMountBackend(iface) {
					continue
				}
				conns, err := repo.Connected(up.InstanceName(), slotInfo.Name)
				if err != nil {
					return nil, err
				}
				for _, cref := range conns {
					if all[cref.PlugRef.Snap] != nil {
						addAffected(cref.PlugRef.Snap, up.InstanceName(), true, false)
					}
				}
			}
		}
		for _, plugInfo := range up.Plugs {
			iface := repo.Interface(plugInfo.Interface)
			if iface == nil {
				return nil, fmt.Errorf("internal error: unknown interface %s", plugInfo.Interface)
			}
			if !usesMountBackend(iface) {
				continue
			}
			conns, err := repo.Connected(up.InstanceName(), plugInfo.Name)
			if err != nil {
				return nil, err
			}
			for _, cref := range conns {
				if all[cref.SlotRef.Snap] != nil {
					addAffected(cref.SlotRef.Snap, up.InstanceName(), true, false)
				}
			}
		}
	}

	return affected, nil
}

// XXX: this is too wide and affects all commonInterface-based interfaces; we
// need metadata on the relevant interfaces.
func usesMountBackend(iface interfaces.Interface) bool {
	type definer1 interface {
		MountConnectedSlot(*mount.Specification, *interfaces.ConnectedPlug, *interfaces.ConnectedSlot) error
	}
	type definer2 interface {
		MountConnectedPlug(*mount.Specification, *interfaces.ConnectedPlug, *interfaces.ConnectedSlot) error
	}
	type definer3 interface {
		MountPermanentPlug(*mount.Specification, *snap.PlugInfo) error
	}
	type definer4 interface {
		MountPermanentSlot(*mount.Specification, *snap.SlotInfo) error
	}

	if _, ok := iface.(definer1); ok {
		return true
	}
	if _, ok := iface.(definer2); ok {
		return true
	}
	if _, ok := iface.(definer3); ok {
		return true
	}
	if _, ok := iface.(definer4); ok {
		return true
	}
	return false
}

// createGateAutoRefreshHooks creates gate-auto-refresh hooks for all affectedSnaps.
// The hooks will have their context data set from affectedSnapInfo flags (base, restart).
// Hook tasks will be chained to run sequentially.
func createGateAutoRefreshHooks(st *state.State, affectedSnaps map[string]*affectedSnapInfo) *state.TaskSet {
	ts := state.NewTaskSet()
	var prev *state.Task
	// sort names for easy testing
	names := make([]string, 0, len(affectedSnaps))
	for snapName := range affectedSnaps {
		names = append(names, snapName)
	}
	sort.Strings(names)
	for _, snapName := range names {
		affected := affectedSnaps[snapName]
		hookTask := SetupGateAutoRefreshHook(st, snapName, affected.Base, affected.Restart, affected.AffectingSnaps)
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

	held, err := heldSnaps(gatingTask.State())
	if err != nil {
		return nil, err
	}

	var skipped []string
	var candidates []*refreshCandidate
	for _, s := range snaps {
		if !held[s.InstanceName()] {
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
