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
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

var gateAutoRefreshHookName = "gate-auto-refresh"

// cumulative hold time for snaps other than self
const maxOtherHoldDuration = time.Hour * 48

type holdInfo struct {
	// FirstHeld keeps the time when the given snap was first held for refresh by a gating snap.
	FirstHeld time.Time
	// HoldUntil stores the desired end time for holding.
	HoldUntil time.Time
}

var timeNow = func() time.Time {
	return time.Now()
}

func lastRefreshed(st *state.State, snapName string) (*time.Time, error) {
	var snapst SnapState
	if err := Get(st, snapName, &snapst); err != nil {
		return nil, fmt.Errorf("internal error, cannot get snap %q: %v", snapName, err)
	}
	if snapst.LastRefresh != nil {
		return snapst.LastRefresh, nil
	}
	inf, err := snapst.CurrentInfo()
	if err != nil {
		return nil, err
	}
	fst, err := os.Stat(inf.MountFile())
	if err != nil {
		return nil, err
	}
	t := fst.ModTime()
	return &t, nil
}

func refreshGating(st *state.State) (map[string]map[string]*holdInfo, error) {
	// affecting snap -> gating snap(s) -> first-held/hold-until time
	var gating map[string]map[string]*holdInfo
	err := st.Get("refresh-gating", &gating)
	if err != nil && err != state.ErrNoState {
		return nil, fmt.Errorf("internal error: cannot get refresh-gating: %v", err)
	}
	if err == state.ErrNoState {
		return make(map[string]map[string]*holdInfo), nil
	}
	return gating, nil
}

// HoldError contains the details of snaps that cannot to be held.
type HoldError struct {
	SnapsInError map[string]error
}

func (h *HoldError) Error() string {
	l := []string{""}
	for _, e := range h.SnapsInError {
		l = append(l, e.Error())
	}
	return fmt.Sprintf("cannot hold some snaps:%s", strings.Join(l, "\n - "))
}

// HoldRefresh marks affectingSnaps as held for refresh for up to holdTime.
// HoldTime of zero denotes maximum allowed hold time.
// Holding may fail for only some snaps in which case HoldError is returned and
// it contains the details of failed ones.
func HoldRefresh(st *state.State, gatingSnap string, holdTime time.Time, affectingSnaps ...string) error {
	gating, err := refreshGating(st)
	if err != nil {
		return err
	}
	herr := &HoldError{
		SnapsInError: make(map[string]error),
	}
	now := timeNow()
	for _, affecting := range affectingSnaps {
		refreshed, err := lastRefreshed(st, affecting)
		if err != nil {
			return err
		}
		if now.Sub(*refreshed) > maxPostponement {
			herr.SnapsInError[affecting] = fmt.Errorf("cannot hold the refresh of snap %q, maximum postponement time exceeded", affecting)
			continue
		}
		if _, ok := gating[affecting]; !ok {
			gating[affecting] = make(map[string]*holdInfo)
		}
		hold, ok := gating[affecting][gatingSnap]
		if !ok {
			hold = &holdInfo{
				FirstHeld: now,
			}
		}
		// determine the maximum time the snap can be held by this gating snap.
		var maxHoldDur time.Duration
		if affecting == gatingSnap {
			maxHoldDur = maxPostponement
		} else {
			maxHoldDur = maxOtherHoldDuration
		}
		var holdDur time.Duration
		// explicit hold time requested
		if !holdTime.IsZero() {
			holdDur = holdTime.Sub(hold.FirstHeld)
			if holdDur > maxHoldDur {
				herr.SnapsInError[affecting] = fmt.Errorf("requested holding time %s exceeds maximum holding time for snap %q", holdTime, affecting)
				continue
			}
		} else {
			holdDur = maxHoldDur
		}
		// HoldUntil is relative to the time the snap was first held, so in case
		// of consecutive refreshes and hold attempts this cannot go beyond
		// maximum allowed hold time.
		hold.HoldUntil = hold.FirstHeld.Add(holdDur)
		gating[affecting][gatingSnap] = hold
	}
	st.Set("refresh-gating", gating)
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
	for affectingSnap, gatingSnaps := range gating {
		if _, ok := gatingSnaps[gatingSnap]; ok {
			delete(gatingSnaps, gatingSnap)
			changed = true
		}
		if len(gating[affectingSnap]) == 0 {
			delete(gating, affectingSnap)
		}
	}

	if changed {
		st.Set("refresh-gating", gating)
	}
	return nil
}

// resetGating resets gating information by:
// - removing affecting snaps whose held time expired.
// - removing affecting snaps that are not in candidates (meaning there is no update for them anymore).
func resetGating(st *state.State, candidates map[string]*refreshCandidate) error {
	gating, err := refreshGating(st)
	if err != nil {
		return err
	}

	// optimize some edge cases
	if len(gating) == 0 {
		return nil
	}
	if len(candidates) == 0 {
		gating = map[string]map[string]*holdInfo{}
		st.Set("refresh-gating", gating)
		return nil
	}

	now := timeNow()

	var changed bool
	for affectingSnap, gatingSnaps := range gating {
		if candidates[affectingSnap] == nil {
			// the snap doesn't have an update anymore, forget it
			delete(gating, affectingSnap)
			changed = true
			continue
		}

		for gatingSnap, tm := range gatingSnaps {
			if tm.HoldUntil.Before(now) {
				delete(gatingSnaps, gatingSnap)
				changed = true
			}
		}
		// after deleting gating snaps we may end up with empty map under
		// gating[affectingSnap], so remove it.
		if len(gatingSnaps) == 0 {
			delete(gating, affectingSnap)
		}
	}
	if changed {
		st.Set("refresh-gating", gating)
	}
	return nil
}

// resetGatingForRefreshed resets gating information by removing refreshedSnaps
// (they are not held anymore). This should be called for all successfully
// refreshed snaps.
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
		st.Set("refresh-gating", gating)
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

	held := make(map[string]bool)
	for affecting, gatingSnaps := range gating {
		if len(gatingSnaps) > 0 {
			held[affecting] = true
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
	for snapName, affected := range affectedSnaps {
		hookTask := SetupGateAutoRefreshHook(st, snapName, affected.Base, affected.Restart)
		// XXX: it should be fine to run the hooks in parallel
		if prev != nil {
			hookTask.WaitFor(prev)
		}
		ts.AddTask(hookTask)
		prev = hookTask
	}
	return ts
}
