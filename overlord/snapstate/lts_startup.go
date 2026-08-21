// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	snapchannel "github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/ltstrack"
)

// snapdLTSTransitionRetryDelay is how long to wait after a failed quiet-device
// LTS schedule before trying again.
var snapdLTSTransitionRetryDelay = 15 * time.Minute

const (
	snapdLTSInjectedChangeAttr = "snapd-lts-injected"
	snapdLTSVehicleLinkAttr    = "snapd-lts-vehicle-link"
)

var errNoDeviceCtx = errors.New("device context hook is not registered")

// ShouldSkipStatePatches reports whether load-time state patches must not run
// because this process is an LTS-aware vehicle that still has to leave
// latest (or another transition track). Callers hold the state lock.
//
// An in-flight snapd setup with ExplicitChannel is a user pin: patches apply.
func ShouldSkipStatePatches(st *state.State) bool {
	if snapdInFlightHasExplicitChannel(st) {
		return false
	}
	needed, err := snapdNeedsLTSJump(st)
	return err == nil && needed
}

// maybeInjectSnapdLTSHop, if this start is the unaware post-restart vehicle,
// injects a through-link store refresh onto the LTS track into the in-flight
// change and restricts the task runner to that change until the next process.
// Callers hold the state lock. TaskRunner.Ensure must not have run yet.
func (m *SnapManager) maybeInjectSnapdLTSHop() error {
	needed, err := snapdNeedsLTSJump(m.state)
	if err != nil || !needed {
		return nil
	}

	veh, ok := findSnapdLTSVehicle(m.state)
	if !ok {
		return nil
	}

	if veh.origSup.ExplicitChannel {
		return nil
	}

	var injected bool
	if err := veh.chg.Get(snapdLTSInjectedChangeAttr, &injected); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if injected {
		return m.restrictToVehicle(veh.chg)
	}

	if err := m.injectSnapdLTSHop(veh); err != nil {
		return err
	}
	return m.restrictToVehicle(veh.chg)
}

func (m *SnapManager) restrictToVehicle(chg *state.Change) error {
	if m.runner == nil {
		return fmt.Errorf("internal error: snap manager has no task runner")
	}
	if err := m.runner.RestrictToChange(chg); err != nil {
		return err
	}
	return nil
}

type snapdLTSVehicle struct {
	chg       *state.Change
	linkSnap  *state.Task
	origSetup *state.Task
	origSup   *SnapSetup
}

func findSnapdLTSVehicle(st *state.State) (snapdLTSVehicle, bool) {
	for _, chg := range st.Changes() {
		if chg.IsReady() {
			continue
		}
		link, origSetup, origSup, ok := snapdPostRestartSuffixPending(chg)
		if !ok {
			continue
		}
		return snapdLTSVehicle{
			chg:       chg,
			linkSnap:  link,
			origSetup: origSetup,
			origSup:   origSup,
		}, true
	}
	return snapdLTSVehicle{}, false
}

func snapdPostRestartSuffixPending(chg *state.Change) (link, origSetup *state.Task, origSup *SnapSetup, ok bool) {
	var linkDone *state.Task
	suffixPending := false
	for _, tsk := range chg.Tasks() {
		snapsup, err := TaskSnapSetup(tsk)
		if err != nil || snapsup.InstanceName() != "snapd" {
			continue
		}
		if tsk.Has("snap-setup") && (tsk.Kind() == "download-snap" || tsk.Kind() == "prepare-snap") && origSetup == nil {
			origSetup = tsk
			origSup = snapsup
		}
		switch tsk.Kind() {
		case "link-snap":
			if tsk.Status() == state.DoneStatus && linkDone == nil {
				linkDone = tsk
			}
		case "auto-connect", "check-reseal":
			switch tsk.Status() {
			case state.DoStatus, state.DoingStatus:
				suffixPending = true
			}
		}
	}
	if linkDone == nil || !suffixPending || origSetup == nil {
		return nil, nil, nil, false
	}
	return linkDone, origSetup, origSup, true
}

func (m *SnapManager) injectSnapdLTSHop(veh snapdLTSVehicle) error {
	st := m.state

	target, err := snapdLTSTargetChannel(st)
	if err != nil {
		return err
	}

	var snapst SnapState
	if err := Get(st, "snapd", &snapst); err != nil {
		return err
	}

	version := veh.origSup.Version
	if info, err := snapst.CurrentInfo(); err == nil && info.Version != "" {
		version = info.Version
	}

	hopSup := ltsHopSnapSetup(veh.origSup, target, version)
	if hopSup.SideInfo == nil || hopSup.SideInfo.SnapID == "" {
		return nil
	}

	installTS, err := doInstallOrPreDownload(st, &snapst, hopSup, nil, installContext{
		StopAfterLink:       true,
		NoRestartBoundaries: true,
		ConflictOptions:     ConflictOptions{FromChange: veh.chg.ID()},
	})
	if err != nil {
		return fmt.Errorf("cannot plan snapd LTS hop onto %q: %v", target, err)
	}

	extras := installTS.ts
	injectedSetup, err := extras.Edge(SnapSetupEdge)
	if err != nil {
		return fmt.Errorf("internal error: snapd LTS hop has no snap-setup task: %v", err)
	}

	extraIDs := make(map[string]bool, len(extras.Tasks()))
	for _, t := range extras.Tasks() {
		extraIDs[t.ID()] = true
	}

	origHalt := veh.linkSnap.HaltTasks()
	injectSnapdLTSTasks(veh.linkSnap, extras)
	retargetSnapdSuffixToInjected(veh.chg, veh.origSetup, injectedSetup, extraIDs)
	markSnapdLTSSecondRestart(extras, origHalt)
	veh.chg.Set(snapdLTSInjectedChangeAttr, true)
	veh.linkSnap.Set(snapdLTSVehicleLinkAttr, true)

	logger.Noticef("injected snapd LTS hop onto %q into change %s", target, veh.chg.ID())
	return nil
}

// injectSnapdLTSTasks is InjectTasks without joining the original lane.
// Pending suffix (and mixed-refresh followers) join the extras lane so a
// failed hop Holds them without aborting the Done vehicle link-snap.
func injectSnapdLTSTasks(linkSnap *state.Task, extras *state.TaskSet) {
	st := linkSnap.State()
	lane := st.NewLane()
	extras.JoinLane(lane)

	chg := linkSnap.Change()
	if chg != nil {
		chg.AddAll(extras)
	}
	for _, t := range linkSnap.HaltTasks() {
		t.WaitAll(extras)
	}
	extras.WaitFor(linkSnap)

	if chg == nil {
		return
	}
	extraIDs := make(map[string]bool, len(extras.Tasks()))
	for _, t := range extras.Tasks() {
		extraIDs[t.ID()] = true
	}
	// Already-Done vehicle tasks (download/mount/link) join a fully-live
	// lane so AbortLanes of the original refresh lane cannot undo them
	// back to unaware snapd (TransactionPerSnap / AllSnaps).
	frozen := st.NewLane()
	for _, t := range chg.Tasks() {
		if extraIDs[t.ID()] {
			continue
		}
		if t.Status().Ready() {
			t.JoinLane(frozen)
			continue
		}
		t.JoinLane(lane)
	}
}

func markSnapdLTSSecondRestart(extras *state.TaskSet, origHalt []*state.Task) {
	for _, t := range origHalt {
		switch t.Kind() {
		case "auto-connect", "check-reseal":
			t.Set("finish-restart", true)
		}
	}
	for _, t := range extras.Tasks() {
		if t.Kind() != "link-snap" {
			continue
		}
		for _, h := range t.HaltTasks() {
			h.Set("finish-restart", true)
		}
	}
}

// preserveSnapdLTSVehicleLink reports whether undo of this link-snap must
// be skipped so the device stays on the already-linked LTS-aware vehicle
// instead of rolling back to unaware snapd.
func preserveSnapdLTSVehicleLink(t *state.Task) bool {
	var preserve bool
	if err := t.Get(snapdLTSVehicleLinkAttr, &preserve); err != nil || !preserve {
		return false
	}
	return true
}

// ensureSnapdLTSTrackTransition starts a normal snapd refresh onto the LTS
// track when the device is already running an LTS-aware snapd on a transition
// track with nothing in flight. Mixed in-flight refreshes are handled by
// maybeInjectSnapdLTSHop instead.
func (m *SnapManager) ensureSnapdLTSTrackTransition() error {
	m.state.Lock()
	defer m.state.Unlock()

	var seeded bool
	if err := m.state.Get("seeded", &seeded); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if !seeded {
		return nil
	}

	needed, err := snapdNeedsLTSJump(m.state)
	if err != nil || !needed {
		return nil
	}
	if _, ok := findSnapdLTSVehicle(m.state); ok {
		return nil
	}
	if changeInFlight(m.state) {
		return nil
	}

	var lastAttempt time.Time
	if err := m.state.Get("snapd-lts-transition-last-retry-time", &lastAttempt); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if !lastAttempt.IsZero() && lastAttempt.Add(snapdLTSTransitionRetryDelay).After(timeNow()) {
		return nil
	}

	if err := m.scheduleSnapdLTSTrackTransition(); err != nil {
		var conflict *ChangeConflictError
		if errors.As(err, &conflict) {
			return nil
		}
		logger.Noticef("cannot schedule snapd LTS track transition: %v", err)
		m.state.Set("snapd-lts-transition-last-retry-time", timeNow())
		return nil
	}
	return nil
}

func (m *SnapManager) scheduleSnapdLTSTrackTransition() error {
	st := m.state

	target, err := snapdLTSTargetChannel(st)
	if err != nil {
		return err
	}

	var snapst SnapState
	if err := Get(st, "snapd", &snapst); err != nil {
		return err
	}
	si := snapst.CurrentSideInfo()
	if si == nil || si.SnapID == "" {
		return nil
	}

	version := ""
	if info, err := snapst.CurrentInfo(); err == nil {
		version = info.Version
	}

	hopSup := ltsHopSnapSetup(&SnapSetup{
		Channel:  snapst.TrackingChannel,
		Type:     snap.TypeSnapd,
		Version:  version,
		SideInfo: si,
	}, target, version)

	installTS, err := doInstallOrPreDownload(st, &snapst, hopSup, nil, installContext{})
	if err != nil {
		return err
	}

	chg := st.NewChange(snapdLTSTrackTransitionChangeKind, fmt.Sprintf(i18n.G("Switch snapd onto LTS track %q"), target))
	chg.AddAll(installTS.ts)
	logger.Noticef("scheduled snapd refresh onto LTS track %q (change %s)", target, chg.ID())
	return nil
}

func ltsHopSnapSetup(orig *SnapSetup, targetChannel, version string) *SnapSetup {
	hop := *orig
	hop.Channel = targetChannel
	hop.ExplicitChannel = false
	hop.ExplicitRevision = false
	hop.Version = version
	hop.SnapPath = ""
	hop.DownloadInfo = nil
	si := snap.SideInfo{}
	if orig.SideInfo != nil {
		si = *orig.SideInfo
	}
	si.Revision = snap.Revision{}
	hop.SideInfo = &si
	if hop.Type == "" {
		hop.Type = snap.TypeSnapd
	}
	return &hop
}

func retargetSnapdSuffixToInjected(chg *state.Change, origSetup, injectedSetup *state.Task, extraIDs map[string]bool) {
	origID := origSetup.ID()
	injectedID := injectedSetup.ID()
	for _, t := range chg.Tasks() {
		if extraIDs[t.ID()] || t.Status().Ready() || t.ID() == origID {
			continue
		}
		if t.Has("snap-setup") {
			// local snap-setup is this task's own revision (discards)
			continue
		}
		var id string
		if err := t.Get("snap-setup-task", &id); err != nil || id != origID {
			continue
		}
		t.Set("snap-setup-task", injectedID)
	}
}

func snapdInFlightHasExplicitChannel(st *state.State) bool {
	for _, chg := range st.Changes() {
		if chg.IsReady() {
			continue
		}
		for _, tsk := range chg.Tasks() {
			if !tsk.Has("snap-setup") {
				continue
			}
			snapsup, err := TaskSnapSetup(tsk)
			if err != nil || snapsup.InstanceName() != "snapd" {
				continue
			}
			if snapsup.ExplicitChannel {
				return true
			}
		}
	}
	return false
}

func snapdNeedsLTSJump(st *state.State) (bool, error) {
	var snapst SnapState
	if err := Get(st, "snapd", &snapst); err != nil {
		if errors.Is(err, state.ErrNoState) {
			return false, nil
		}
		return false, err
	}
	channel := snapst.TrackingChannel
	if channel == "" {
		channel = "latest/stable"
	}

	model, err := snapdLTSModel(st)
	if errors.Is(err, errNoDeviceCtx) {
		if release.OnClassic {
			return false, nil
		}
		return trackingIsLTSTransition(channel), nil
	}
	if err != nil {
		return false, err
	}

	target, err := ltstrack.Resolve(model, channel, nil)
	if err != nil {
		return false, err
	}
	parsed, err := snapchannel.ParseVerbatim(channel, "-")
	if err != nil {
		return false, err
	}
	return target != parsed.Clean().String(), nil
}

func snapdLTSTargetChannel(st *state.State) (string, error) {
	var snapst SnapState
	if err := Get(st, "snapd", &snapst); err != nil {
		return "", err
	}
	channel := snapst.TrackingChannel
	if channel == "" {
		channel = "latest/stable"
	}
	model, err := snapdLTSModel(st)
	if err != nil {
		return "", err
	}
	return ltstrack.Resolve(model, channel, nil)
}

func snapdLTSModel(st *state.State) (*asserts.Model, error) {
	if DeviceCtx == nil {
		return nil, errNoDeviceCtx
	}
	dctx, err := DeviceCtx(st, nil, nil)
	if err != nil {
		return nil, err
	}
	if dctx == nil {
		return nil, state.ErrNoState
	}
	return dctx.Model(), nil
}

// trackingIsLTSTransition reports whether channel's track is a from-key in the
// running map (a transition track), used when the model is not yet available.
func trackingIsLTSTransition(channel string) bool {
	parsed, err := snapchannel.ParseVerbatim(channel, "-")
	if err != nil {
		return false
	}
	track := parsed.Track
	if track == "" {
		track = "latest"
	}
	m, _, err := snap.SnapdLTSTrackMapFromThis()
	if err != nil || len(m) == 0 {
		return false
	}
	for _, byBase := range m {
		if _, ok := byBase[track]; ok {
			return true
		}
	}
	return false
}
