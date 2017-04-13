// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	"strconv"
	"strings"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

// TaskSnapSetup returns the SnapSetup with task params hold by or referred to by the the task.
func TaskSnapSetup(t *state.Task) (*SnapSetup, error) {
	var snapsup SnapSetup

	err := t.Get("snap-setup", &snapsup)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if err == nil {
		return &snapsup, nil
	}

	var id string
	err = t.Get("snap-setup-task", &id)
	if err != nil {
		return nil, err
	}

	ts := t.State().Task(id)
	if err := ts.Get("snap-setup", &snapsup); err != nil {
		return nil, err
	}
	return &snapsup, nil
}

func snapSetupAndState(t *state.Task) (*SnapSetup, *SnapState, error) {
	snapsup, err := TaskSnapSetup(t)
	if err != nil {
		return nil, nil, err
	}
	var snapst SnapState
	err = Get(t.State(), snapsup.Name(), &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, nil, err
	}
	return snapsup, &snapst, nil
}

/* State Locking

   do* / undo* handlers should usually lock the state just once with:

	st.Lock()
	defer st.Unlock()

   For tasks doing slow operations (long i/o, networking operations) it's OK
   to unlock the state temporarily:

        st.Unlock()
        err := slowIOOp()
        st.Lock()
        if err != nil {
           ...
        }

    but if a task Get and then Set the SnapState of a snap it must avoid
    releasing the state lock in between, other tasks might have
    reasons to update the SnapState independently:

        // DO NOT DO THIS!:
        snapst := ...
        snapst.Attr = ...
        st.Unlock()
        ...
        st.Lock()
        Set(st, snapName, snapst)

    if a task really needs to mix mutating a SnapState and releasing the state
    lock it should be serialized at the task runner level, see
    SnapManger.blockedTask and TaskRunner.SetBlocked

*/

func (m *SnapManager) doPrepareSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	if snapsup.Revision().Unset() {
		// Local revisions start at -1 and go down.
		revision := snapst.LocalRevision()
		if revision.Unset() || revision.N > 0 {
			revision = snap.R(-1)
		} else {
			revision.N--
		}
		if !revision.Local() {
			panic("internal error: invalid local revision built: " + revision.String())
		}
		snapsup.SideInfo.Revision = revision
	}

	t.Set("snap-setup", snapsup)
	return nil
}

func (m *SnapManager) undoPrepareSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	snapsup, err := TaskSnapSetup(t)
	if err != nil {
		return err
	}

	if snapsup.SideInfo == nil || snapsup.SideInfo.RealName == "" {
		return nil
	}

	var logMsg []string
	var snapSetup string
	dupSig := []string{"snap-install:"}
	chg := t.Change()
	logMsg = append(logMsg, fmt.Sprintf("change %q: %q", chg.Kind(), chg.Summary()))
	for _, t := range chg.Tasks() {
		// TODO: report only tasks in intersecting lanes?
		tintro := fmt.Sprintf("%s: %s", t.Kind(), t.Status())
		logMsg = append(logMsg, tintro)
		dupSig = append(dupSig, tintro)
		if snapsup, err := TaskSnapSetup(t); err == nil && snapsup.SideInfo != nil {
			snapSetup1 := fmt.Sprintf(" snap-setup: %q (%v) %q", snapsup.SideInfo.RealName, snapsup.SideInfo.Revision, snapsup.SideInfo.Channel)
			if snapSetup1 != snapSetup {
				snapSetup = snapSetup1
				logMsg = append(logMsg, snapSetup)
				dupSig = append(dupSig, fmt.Sprintf(" snap-setup: %q", snapsup.SideInfo.RealName))
			}
		}
		for _, l := range t.Log() {
			// cut of the rfc339 timestamp to ensure duplicate
			// detection works in daisy
			tStampLen := strings.Index(l, " ")
			if tStampLen < 0 {
				continue
			}
			// not tStampLen+1 because the indent is nice
			entry := l[tStampLen:]
			logMsg = append(logMsg, entry)
			dupSig = append(dupSig, entry)
		}
	}

	var ubuntuCoreTransitionCount int
	err = st.Get("ubuntu-core-transition-retry", &ubuntuCoreTransitionCount)
	if err != nil && err != state.ErrNoState {
		return err
	}
	extra := map[string]string{
		"Channel":  snapsup.Channel,
		"Revision": snapsup.SideInfo.Revision.String(),
	}
	if ubuntuCoreTransitionCount > 0 {
		extra["UbuntuCoreTransitionCount"] = strconv.Itoa(ubuntuCoreTransitionCount)
	}

	st.Unlock()
	oopsid, err := errtrackerReport(snapsup.SideInfo.RealName, strings.Join(logMsg, "\n"), strings.Join(dupSig, "\n"), extra)
	st.Lock()
	if err == nil {
		logger.Noticef("Reported install problem for %q as %s", snapsup.SideInfo.RealName, oopsid)
	} else {
		logger.Debugf("Cannot report problem: %s", err)
	}

	return nil
}

func (m *SnapManager) doDownloadSnap(t *state.Task, tomb *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	snapsup, err := TaskSnapSetup(t)
	st.Unlock()
	if err != nil {
		return err
	}

	st.Lock()
	theStore := Store(st)
	user, err := userFromUserID(st, snapsup.UserID)
	st.Unlock()
	if err != nil {
		return err
	}

	meter := NewTaskProgressAdapterUnlocked(t)
	targetFn := snapsup.MountFile()
	if snapsup.DownloadInfo == nil {
		var storeInfo *snap.Info
		// COMPATIBILITY - this task was created from an older version
		// of snapd that did not store the DownloadInfo in the state
		// yet.
		spec := store.SnapSpec{
			Name:     snapsup.Name(),
			Channel:  snapsup.Channel,
			Revision: snapsup.Revision(),
		}
		storeInfo, err = theStore.SnapInfo(spec, user)
		if err != nil {
			return err
		}
		err = theStore.Download(tomb.Context(nil), snapsup.Name(), targetFn, &storeInfo.DownloadInfo, meter, user)
		snapsup.SideInfo = &storeInfo.SideInfo
	} else {
		err = theStore.Download(tomb.Context(nil), snapsup.Name(), targetFn, snapsup.DownloadInfo, meter, user)
	}
	if err != nil {
		return err
	}

	snapsup.SnapPath = targetFn

	// update the snap setup for the follow up tasks
	st.Lock()
	t.Set("snap-setup", snapsup)
	st.Unlock()

	return nil
}

func (m *SnapManager) doMountSnap(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	snapsup, snapst, err := snapSetupAndState(t)
	t.State().Unlock()
	if err != nil {
		return err
	}
	curInfo, err := snapst.CurrentInfo()
	if err != nil && err != ErrNoCurrent {
		return err
	}

	m.backend.CurrentInfo(curInfo)

	if err := checkSnap(t.State(), snapsup.SnapPath, snapsup.SideInfo, curInfo, snapsup.Flags); err != nil {
		return err
	}

	pb := NewTaskProgressAdapterUnlocked(t)
	// TODO Use snapsup.Revision() to obtain the right info to mount
	//      instead of assuming the candidate is the right one.
	if err := m.backend.SetupSnap(snapsup.SnapPath, snapsup.SideInfo, pb); err != nil {
		return err
	}

	// set snapst type for undoMountSnap
	newInfo, err := readInfo(snapsup.Name(), snapsup.SideInfo)
	if err != nil {
		return err
	}
	t.State().Lock()
	t.Set("snap-type", newInfo.Type)
	t.State().Unlock()

	if snapsup.Flags.RemoveSnapPath {
		if err := os.Remove(snapsup.SnapPath); err != nil {
			logger.Noticef("Failed to cleanup %s: %s", snapsup.SnapPath, err)
		}
	}

	return nil
}

func (m *SnapManager) undoMountSnap(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	snapsup, err := TaskSnapSetup(t)
	t.State().Unlock()
	if err != nil {
		return err
	}

	t.State().Lock()
	var typ snap.Type
	err = t.Get("snap-type", &typ)
	t.State().Unlock()
	// backward compatibility
	if err == state.ErrNoState {
		typ = "app"
	} else if err != nil {
		return err
	}

	pb := NewTaskProgressAdapterUnlocked(t)
	return m.backend.UndoSetupSnap(snapsup.placeInfo(), typ, pb)
}

func (m *SnapManager) doUnlinkCurrentSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	oldInfo, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}

	snapst.Active = false

	pb := NewTaskProgressAdapterLocked(t)
	err = m.backend.UnlinkSnap(oldInfo, pb)
	if err != nil {
		return err
	}

	// mark as inactive
	Set(st, snapsup.Name(), snapst)
	return nil
}

func (m *SnapManager) undoUnlinkCurrentSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	oldInfo, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}

	snapst.Active = true
	err = m.backend.LinkSnap(oldInfo)
	if err != nil {
		return err
	}

	// mark as active again
	Set(st, snapsup.Name(), snapst)

	// if we just put back a previous a core snap, request a restart
	// so that we switch executing its snapd
	maybeRestart(t, oldInfo)

	return nil
}

func (m *SnapManager) doCopySnapData(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	snapsup, snapst, err := snapSetupAndState(t)
	t.State().Unlock()
	if err != nil {
		return err
	}

	newInfo, err := readInfo(snapsup.Name(), snapsup.SideInfo)
	if err != nil {
		return err
	}

	oldInfo, err := snapst.CurrentInfo()
	if err != nil && err != ErrNoCurrent {
		return err
	}

	pb := NewTaskProgressAdapterUnlocked(t)
	return m.backend.CopySnapData(newInfo, oldInfo, pb)
}

func (m *SnapManager) undoCopySnapData(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	snapsup, snapst, err := snapSetupAndState(t)
	t.State().Unlock()
	if err != nil {
		return err
	}

	newInfo, err := readInfo(snapsup.Name(), snapsup.SideInfo)
	if err != nil {
		return err
	}

	oldInfo, err := snapst.CurrentInfo()
	if err != nil && err != ErrNoCurrent {
		return err
	}

	pb := NewTaskProgressAdapterUnlocked(t)
	return m.backend.UndoCopySnapData(newInfo, oldInfo, pb)
}

func (m *SnapManager) cleanupCopySnapData(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	if t.Status() != state.DoneStatus {
		// it failed
		return nil
	}

	_, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	info, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}

	m.backend.ClearTrashedData(info)

	return nil
}

func (m *SnapManager) doLinkSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	cand := snapsup.SideInfo
	m.backend.Candidate(cand)

	oldCandidateIndex := snapst.LastIndex(cand.Revision)

	if oldCandidateIndex < 0 {
		snapst.Sequence = append(snapst.Sequence, cand)
	} else if !snapsup.Revert {
		// remove the old candidate from the sequence, add it at the end
		copy(snapst.Sequence[oldCandidateIndex:len(snapst.Sequence)-1], snapst.Sequence[oldCandidateIndex+1:])
		snapst.Sequence[len(snapst.Sequence)-1] = cand
	}

	oldCurrent := snapst.Current
	snapst.Current = cand.Revision
	snapst.Active = true
	oldChannel := snapst.Channel
	if snapsup.Channel != "" {
		snapst.Channel = snapsup.Channel
	}
	oldTryMode := snapst.TryMode
	snapst.TryMode = snapsup.TryMode
	oldDevMode := snapst.DevMode
	snapst.DevMode = snapsup.DevMode
	oldJailMode := snapst.JailMode
	snapst.JailMode = snapsup.JailMode
	oldClassic := snapst.Classic
	snapst.Classic = snapsup.Classic
	if snapsup.Required { // set only on install and left alone on refresh
		snapst.Required = true
	}

	newInfo, err := readInfo(snapsup.Name(), cand)
	if err != nil {
		return err
	}

	// record type
	snapst.SetType(newInfo.Type)

	// XXX: this block is slightly ugly, find a pattern when we have more examples
	err = m.backend.LinkSnap(newInfo)
	if err != nil {
		pb := NewTaskProgressAdapterLocked(t)
		err := m.backend.UnlinkSnap(newInfo, pb)
		if err != nil {
			t.Errorf("cannot cleanup failed attempt at making snap %q available to the system: %v", snapsup.Name(), err)
		}
	}
	if err != nil {
		return err
	}

	// save for undoLinkSnap
	t.Set("old-trymode", oldTryMode)
	t.Set("old-devmode", oldDevMode)
	t.Set("old-jailmode", oldJailMode)
	t.Set("old-classic", oldClassic)
	t.Set("old-channel", oldChannel)
	t.Set("old-current", oldCurrent)
	t.Set("old-candidate-index", oldCandidateIndex)
	// Do at the end so we only preserve the new state if it worked.
	Set(st, snapsup.Name(), snapst)
	// Make sure if state commits and snapst is mutated we won't be rerun
	t.SetStatus(state.DoneStatus)

	// if we just installed a core snap, request a restart
	// so that we switch executing its snapd
	maybeRestart(t, newInfo)

	return nil
}

// maybeRestart will schedule a reboot or restart as needed for the just linked
// snap with info if it's a core or kernel snap.
func maybeRestart(t *state.Task, info *snap.Info) {
	st := t.State()
	if release.OnClassic && info.Type == snap.TypeOS {
		t.Logf("Requested daemon restart.")
		st.RequestRestart(state.RestartDaemon)
	}
	if !release.OnClassic && boot.KernelOrOsRebootRequired(info) {
		t.Logf("Requested system restart.")
		st.RequestRestart(state.RestartSystem)
	}
}

func (m *SnapManager) undoLinkSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	var oldChannel string
	err = t.Get("old-channel", &oldChannel)
	if err != nil {
		return err
	}
	var oldTryMode bool
	err = t.Get("old-trymode", &oldTryMode)
	if err != nil {
		return err
	}
	var oldDevMode bool
	err = t.Get("old-devmode", &oldDevMode)
	if err != nil {
		return err
	}
	var oldJailMode bool
	err = t.Get("old-jailmode", &oldJailMode)
	if err != nil {
		return err
	}
	var oldClassic bool
	err = t.Get("old-classic", &oldClassic)
	if err != nil {
		return err
	}
	var oldCurrent snap.Revision
	err = t.Get("old-current", &oldCurrent)
	if err != nil {
		return err
	}
	var oldCandidateIndex int
	if err := t.Get("old-candidate-index", &oldCandidateIndex); err != nil {
		return err
	}

	isRevert := snapsup.Revert

	// relinking of the old snap is done in the undo of unlink-current-snap
	currentIndex := snapst.LastIndex(snapst.Current)
	if currentIndex < 0 {
		return fmt.Errorf("internal error: cannot find revision %d in %v for undoing the added revision", snapsup.SideInfo.Revision, snapst.Sequence)
	}

	if oldCandidateIndex < 0 {
		snapst.Sequence = append(snapst.Sequence[:currentIndex], snapst.Sequence[currentIndex+1:]...)
	} else if !isRevert {
		oldCand := snapst.Sequence[currentIndex]
		copy(snapst.Sequence[oldCandidateIndex+1:], snapst.Sequence[oldCandidateIndex:])
		snapst.Sequence[oldCandidateIndex] = oldCand
	}
	snapst.Current = oldCurrent
	snapst.Active = false
	snapst.Channel = oldChannel
	snapst.TryMode = oldTryMode
	snapst.DevMode = oldDevMode
	snapst.JailMode = oldJailMode
	snapst.Classic = oldClassic

	newInfo, err := readInfo(snapsup.Name(), snapsup.SideInfo)
	if err != nil {
		return err
	}

	pb := NewTaskProgressAdapterLocked(t)
	err = m.backend.UnlinkSnap(newInfo, pb)
	if err != nil {
		return err
	}

	// mark as inactive
	Set(st, snapsup.Name(), snapst)
	// Make sure if state commits and snapst is mutated we won't be rerun
	t.SetStatus(state.UndoneStatus)
	return nil
}

func (m *SnapManager) doSwitchSnapChannel(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	// switched the tracked channel
	snapst.Channel = snapsup.Channel
	// optionally support switching the current snap channel too, e.g.
	// if a snap is in both stable and candidate with the same revision
	// we can update it here and it will be displayed correctly in the UI
	if snapsup.SideInfo.Channel != "" {
		snapst.CurrentSideInfo().Channel = snapsup.Channel
	}

	Set(st, snapsup.Name(), snapst)
	return nil
}

func (m *SnapManager) startSnapServices(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	_, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	currentInfo, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}

	pb := NewTaskProgressAdapterUnlocked(t)
	st.Unlock()
	err = m.backend.StartSnapServices(currentInfo, pb)
	st.Lock()
	return err
}

func (m *SnapManager) stopSnapServices(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	_, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	currentInfo, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}

	pb := NewTaskProgressAdapterUnlocked(t)
	st.Unlock()
	err = m.backend.StopSnapServices(currentInfo, pb)
	st.Lock()
	return err
}

func (m *SnapManager) doUnlinkSnap(t *state.Task, _ *tomb.Tomb) error {
	// invoked only if snap has a current active revision
	st := t.State()
	st.Lock()
	defer st.Unlock()

	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	info, err := Info(t.State(), snapsup.Name(), snapsup.Revision())
	if err != nil {
		return err
	}

	pb := NewTaskProgressAdapterLocked(t)
	err = m.backend.UnlinkSnap(info, pb)
	if err != nil {
		return err
	}

	// mark as inactive
	snapst.Active = false
	Set(st, snapsup.Name(), snapst)
	return nil
}

func (m *SnapManager) doClearSnapData(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	snapsup, snapst, err := snapSetupAndState(t)
	t.State().Unlock()
	if err != nil {
		return err
	}

	t.State().Lock()
	info, err := Info(t.State(), snapsup.Name(), snapsup.Revision())
	t.State().Unlock()
	if err != nil {
		return err
	}

	if err = m.backend.RemoveSnapData(info); err != nil {
		return err
	}

	// Only remove data common between versions if this is the last version
	if len(snapst.Sequence) == 1 {
		if err = m.backend.RemoveSnapCommonData(info); err != nil {
			return err
		}
	}

	return nil
}

func (m *SnapManager) doDiscardSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	if snapst.Current == snapsup.Revision() && snapst.Active {
		return fmt.Errorf("internal error: cannot discard snap %q: still active", snapsup.Name())
	}

	if len(snapst.Sequence) == 1 {
		snapst.Sequence = nil
		snapst.Current = snap.Revision{}
	} else {
		newSeq := make([]*snap.SideInfo, 0, len(snapst.Sequence))
		for _, si := range snapst.Sequence {
			if si.Revision == snapsup.Revision() {
				// leave out
				continue
			}
			newSeq = append(newSeq, si)
		}
		snapst.Sequence = newSeq
		if snapst.Current == snapsup.Revision() {
			snapst.Current = newSeq[len(newSeq)-1].Revision
		}
	}

	pb := NewTaskProgressAdapterLocked(t)
	typ, err := snapst.Type()
	if err != nil {
		return err
	}
	err = m.backend.RemoveSnapFiles(snapsup.placeInfo(), typ, pb)
	if err != nil {
		t.Errorf("cannot remove snap file %q, will retry in 3 mins: %s", snapsup.Name(), err)
		return &state.Retry{After: 3 * time.Minute}
	}
	if len(snapst.Sequence) == 0 {
		// Remove configuration associated with this snap.
		err = config.DeleteSnapConfig(st, snapsup.Name())
		if err != nil {
			return err
		}
		err = m.backend.DiscardSnapNamespace(snapsup.Name())
		if err != nil {
			t.Errorf("cannot discard snap namespace %q, will retry in 3 mins: %s", snapsup.Name(), err)
			return &state.Retry{After: 3 * time.Minute}
		}
	}

	Set(st, snapsup.Name(), snapst)
	return nil
}

// aliases v2

func (m *SnapManager) doSetAutoAliasesV2(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	snapName := snapsup.Name()
	curInfo, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}

	curAliases := snapst.Aliases
	// TODO: implement --prefer/--unaliased logic
	newAliases, err := refreshAliases(st, curInfo, curAliases)
	if err != nil {
		return err
	}
	_, err = checkAliasesConflicts(st, snapName, snapst.AutoAliasesDisabled, newAliases)
	if err != nil {
		return err
	}

	t.Set("old-aliases-v2", curAliases)
	// noop, except on first install where we need to set this here
	snapst.AliasesPending = true
	snapst.Aliases = newAliases
	Set(st, snapName, snapst)
	return nil
}

func (m *SnapManager) doRemoveAliasesV2(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	snapName := snapsup.Name()

	err = m.backend.RemoveSnapAliases(snapName)
	if err != nil {
		return err
	}

	snapst.AliasesPending = true
	Set(st, snapName, snapst)
	return nil
}

func (m *SnapManager) doSetupAliasesV2(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	snapName := snapsup.Name()
	curAliases := snapst.Aliases

	err = applyAliasesChange(st, snapName, true, nil, snapst.AutoAliasesDisabled, curAliases, m.backend)
	if err != nil {
		return err
	}

	snapst.AliasesPending = false
	Set(st, snapName, snapst)
	return nil
}

func (m *SnapManager) doRefreshAliasesV2(t *state.Task, _ *tomb.Tomb) error {
	// TODO: share code with doSetAutoAliasesV2 once the logic is more complex
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	snapName := snapsup.Name()
	curInfo, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}

	autoDisabled := snapst.AutoAliasesDisabled
	curAliases := snapst.Aliases
	// TODO: implement --prefer/--unaliased logic
	newAliases, err := refreshAliases(st, curInfo, curAliases)
	if err != nil {
		return err
	}
	_, err = checkAliasesConflicts(st, snapName, autoDisabled, newAliases)
	if err != nil {
		return err
	}

	if !snapst.AliasesPending {
		if err := applyAliasesChange(st, snapName, autoDisabled, curAliases, autoDisabled, newAliases, m.backend); err != nil {
			return err
		}
	}

	t.Set("old-aliases-v2", curAliases)
	snapst.Aliases = newAliases
	Set(st, snapName, snapst)
	return nil
}

func (m *SnapManager) undoRefreshAliasesV2(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	var oldAliases map[string]*AliasTarget
	err := t.Get("old-aliases-v2", &oldAliases)
	if err == state.ErrNoState {
		// nothing to do
		return nil
	}
	if err != nil {
		return err
	}
	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	snapName := snapsup.Name()
	curAutoDisabled := snapst.AutoAliasesDisabled
	autoDisabled := curAutoDisabled
	if err = t.Get("old-auto-aliases-disabled", &autoDisabled); err != nil && err != state.ErrNoState {
		return err
	}

	// check if the old states creates conflicts now
	_, err = checkAliasesConflicts(st, snapName, autoDisabled, oldAliases)
	if _, ok := err.(*AliasConflictError); ok {
		// best we can do is reinstate with all aliases disabled
		t.Errorf("cannot reinstate alias state because of conflicts, disabling: %v", err)
		oldAliases = disableAliases(oldAliases)
		autoDisabled = true
	} else if err != nil {
		return err
	}

	if !snapst.AliasesPending {
		curAliases := snapst.Aliases
		if err := applyAliasesChange(st, snapName, curAutoDisabled, curAliases, autoDisabled, oldAliases, m.backend); err != nil {
			return err
		}
	}

	snapst.AutoAliasesDisabled = autoDisabled
	snapst.Aliases = oldAliases
	Set(st, snapName, snapst)
	return nil
}

func (m *SnapManager) doPruneAutoAliasesV2(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	var which []string
	err = t.Get("aliases", &which)
	if err != nil {
		return err
	}
	snapName := snapsup.Name()
	autoDisabled := snapst.AutoAliasesDisabled
	curAliases := snapst.Aliases

	newAliases := pruneAutoAliases(curAliases, which)

	if !snapst.AliasesPending {
		if err := applyAliasesChange(st, snapName, autoDisabled, curAliases, autoDisabled, newAliases, m.backend); err != nil {
			return err
		}
	}

	t.Set("old-aliases-v2", curAliases)
	snapst.Aliases = newAliases
	Set(st, snapName, snapst)
	return nil
}

func (m *SnapManager) doAliasV2(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	var target, alias string
	err = t.Get("target", &target)
	if err != nil {
		return err
	}
	err = t.Get("alias", &alias)
	if err != nil {
		return err
	}

	snapName := snapsup.Name()
	curInfo, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}

	autoDisabled := snapst.AutoAliasesDisabled
	curAliases := snapst.Aliases
	newAliases, err := manualAlias(curInfo, curAliases, target, alias)
	if err != nil {
		return err
	}
	_, err = checkAliasesConflicts(st, snapName, autoDisabled, newAliases)
	if err != nil {
		return err
	}

	if !snapst.AliasesPending {
		if err := applyAliasesChange(st, snapName, autoDisabled, curAliases, autoDisabled, newAliases, m.backend); err != nil {
			return err
		}
	}

	t.Set("old-aliases-v2", curAliases)
	snapst.Aliases = newAliases
	Set(st, snapName, snapst)
	return nil
}

func (m *SnapManager) doUnaliasV2(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	var alias string
	err = t.Get("alias", &alias)
	if err != nil {
		return err
	}
	snapName := snapsup.Name()

	oldAutoDisabled := snapst.AutoAliasesDisabled
	autoDisabled := oldAutoDisabled
	oldAliases := snapst.Aliases
	var newAliases map[string]*AliasTarget
	if alias == "" {
		newAliases = disableAliases(oldAliases)
		autoDisabled = true
	} else {
		newAliases, err = manualUnalias(oldAliases, alias)
		if err != nil {
			return err
		}
	}

	if !snapst.AliasesPending {
		if err := applyAliasesChange(st, snapName, oldAutoDisabled, oldAliases, autoDisabled, newAliases, m.backend); err != nil {
			return err
		}
	}

	if oldAutoDisabled != autoDisabled {
		t.Set("old-auto-aliases-disabled", oldAutoDisabled)
		snapst.AutoAliasesDisabled = autoDisabled
	}
	t.Set("old-aliases-v2", oldAliases)
	snapst.Aliases = newAliases
	Set(st, snapName, snapst)
	return nil
}
