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
	"github.com/snapcore/snapd/overlord/snapstate/backend"
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

	// Make a copy of configuration of given snap revision
	if err = config.SaveRevisionConfig(st, snapsup.Name(), snapst.Current); err != nil {
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

	// Restore configuration of the target revision (if available) on revert
	if snapsup.Revert {
		if err = config.RestoreRevisionConfig(st, snapsup.Name(), snapsup.Revision()); err != nil {
			return err
		}
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

	if err = config.RestoreRevisionConfig(st, snapsup.Name(), oldCurrent); err != nil {
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
	if err = config.DiscardRevisionConfig(st, snapsup.Name(), snapsup.Revision()); err != nil {
		return err
	}
	Set(st, snapsup.Name(), snapst)
	return nil
}

/* aliases v2

aliases v2 implementation uses the following tasks:

  * for install/refresh/remove/enable/disable etc

    - remove-aliases: remove aliases of a snap from disk and mark them pending

    - setup-aliases: (re)creates aliases from snap state, mark them as
      not pending

    - set-auto-aliases: updates aliases snap state based on the
      snap-declaration and current revision info of the snap

  * for refresh & when the snap-declaration aliases change without a
    new revision

    - refresh-aliases: updates aliases snap state and updates them on disk too;
      its undo is used generically by other tasks as well

    - prune-auto-aliases: used for the special case of automatic
      aliases transferred from one snap to another to prune them from
      the source snaps to avoid conflicts in later operations

  * for alias/unalias/prefer:

    - alias: creates a manual alias

    - unalias: removes a manual alias

    - disable-aliases: disable the automatic aliases of a snap and
      removes all manual ones as well

    - prefer-aliases: enables the automatic aliases of a snap after
      disabling any other snap conflicting aliases

*/

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
	_, err = checkAliasesConflicts(st, snapName, snapst.AutoAliasesDisabled, newAliases, nil)
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

	_, _, err = applyAliasesChange(snapName, true, nil, snapst.AutoAliasesDisabled, curAliases, m.backend, false)
	if err != nil {
		return err
	}

	snapst.AliasesPending = false
	Set(st, snapName, snapst)
	return nil
}

func (m *SnapManager) doRefreshAliasesV2(t *state.Task, _ *tomb.Tomb) error {
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
	newAliases, err := refreshAliases(st, curInfo, curAliases)
	if err != nil {
		return err
	}
	_, err = checkAliasesConflicts(st, snapName, autoDisabled, newAliases, nil)
	if err != nil {
		return err
	}

	if !snapst.AliasesPending {
		if _, _, err := applyAliasesChange(snapName, autoDisabled, curAliases, autoDisabled, newAliases, m.backend, false); err != nil {
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

	var otherSnapDisabled map[string]*otherDisabledAliases
	if err = t.Get("other-disabled-aliases", &otherSnapDisabled); err != nil && err != state.ErrNoState {
		return err
	}

	// check if the old states creates conflicts now
	_, err = checkAliasesConflicts(st, snapName, autoDisabled, oldAliases, nil)
	if _, ok := err.(*AliasConflictError); ok {
		// best we can do is reinstate with all aliases disabled
		t.Errorf("cannot reinstate alias state because of conflicts, disabling: %v", err)
		oldAliases, _ = disableAliases(oldAliases)
		autoDisabled = true
	} else if err != nil {
		return err
	}

	if !snapst.AliasesPending {
		curAliases := snapst.Aliases
		if _, _, err := applyAliasesChange(snapName, curAutoDisabled, curAliases, autoDisabled, oldAliases, m.backend, false); err != nil {
			return err
		}
	}

	snapst.AutoAliasesDisabled = autoDisabled
	snapst.Aliases = oldAliases
	newSnapStates := make(map[string]*SnapState, 1+len(otherSnapDisabled))
	newSnapStates[snapName] = snapst

	// if we disabled other snap aliases try to undo that
	conflicting := make(map[string]bool, len(otherSnapDisabled))
	otherCurSnapStates := make(map[string]*SnapState, len(otherSnapDisabled))
	for otherSnap, otherDisabled := range otherSnapDisabled {
		var otherSnapState SnapState
		err := Get(st, otherSnap, &otherSnapState)
		if err != nil {
			return err
		}
		otherCurInfo, err := otherSnapState.CurrentInfo()
		if err != nil {
			return err
		}

		otherCurSnapStates[otherSnap] = &otherSnapState

		autoDisabled := otherSnapState.AutoAliasesDisabled
		if otherDisabled.Auto {
			// automatic aliases of other were disabled, undo that
			autoDisabled = false
		}
		otherAliases := reenableAliases(otherCurInfo, otherSnapState.Aliases, otherDisabled.Manual)
		// check for conflicts taking into account
		// re-enabled aliases
		conflicts, err := checkAliasesConflicts(st, otherSnap, autoDisabled, otherAliases, newSnapStates)
		if _, ok := err.(*AliasConflictError); ok {
			conflicting[otherSnap] = true
			for conflictSnap := range conflicts {
				conflicting[conflictSnap] = true
			}
		} else if err != nil {
			return err
		}

		newSnapState := otherSnapState
		newSnapState.Aliases = otherAliases
		newSnapState.AutoAliasesDisabled = autoDisabled
		newSnapStates[otherSnap] = &newSnapState
	}

	// apply non-conflicting other
	for otherSnap, otherSnapState := range otherCurSnapStates {
		if conflicting[otherSnap] {
			// keep as it was
			continue
		}
		newSnapSt := newSnapStates[otherSnap]
		if !otherSnapState.AliasesPending {
			if _, _, err := applyAliasesChange(otherSnap, otherSnapState.AutoAliasesDisabled, otherSnapState.Aliases, newSnapSt.AutoAliasesDisabled, newSnapSt.Aliases, m.backend, false); err != nil {
				return err
			}
		}
	}

	for snapName, snapst := range newSnapStates {
		if conflicting[snapName] {
			// keep as it was
			continue
		}
		Set(st, snapName, snapst)
	}
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
		if _, _, err := applyAliasesChange(snapName, autoDisabled, curAliases, autoDisabled, newAliases, m.backend, false); err != nil {
			return err
		}
	}

	t.Set("old-aliases-v2", curAliases)
	snapst.Aliases = newAliases
	Set(st, snapName, snapst)
	return nil
}

type changedAlias struct {
	Snap  string `json:"snap"`
	App   string `json:"app"`
	Alias string `json:"alias"`
}

func aliasesTrace(t *state.Task, added, removed []*backend.Alias) error {
	chg := t.Change()
	var data map[string]interface{}
	err := chg.Get("api-data", &data)
	if err != nil && err != state.ErrNoState {
		return err
	}
	if len(data) == 0 {
		data = make(map[string]interface{})
	}

	curAdded, _ := data["aliases-added"].([]interface{})
	for _, a := range added {
		snap, app := snap.SplitSnapApp(a.Target)
		curAdded = append(curAdded, &changedAlias{
			Snap:  snap,
			App:   app,
			Alias: a.Name,
		})
	}
	data["aliases-added"] = curAdded

	curRemoved, _ := data["aliases-removed"].([]interface{})
	for _, a := range removed {
		snap, app := snap.SplitSnapApp(a.Target)
		curRemoved = append(curRemoved, &changedAlias{
			Snap:  snap,
			App:   app,
			Alias: a.Name,
		})
	}
	data["aliases-removed"] = curRemoved

	chg.Set("api-data", data)
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
	_, err = checkAliasesConflicts(st, snapName, autoDisabled, newAliases, nil)
	if err != nil {
		return err
	}

	added, removed, err := applyAliasesChange(snapName, autoDisabled, curAliases, autoDisabled, newAliases, m.backend, snapst.AliasesPending)
	if err != nil {
		return err
	}
	if err := aliasesTrace(t, added, removed); err != nil {
		return err
	}

	t.Set("old-aliases-v2", curAliases)
	snapst.Aliases = newAliases
	Set(st, snapName, snapst)
	return nil
}

func (m *SnapManager) doDisableAliasesV2(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	snapName := snapsup.Name()

	oldAutoDisabled := snapst.AutoAliasesDisabled
	oldAliases := snapst.Aliases
	newAliases, _ := disableAliases(oldAliases)

	added, removed, err := applyAliasesChange(snapName, oldAutoDisabled, oldAliases, true, newAliases, m.backend, snapst.AliasesPending)
	if err != nil {
		return err
	}
	if err := aliasesTrace(t, added, removed); err != nil {
		return err
	}

	t.Set("old-auto-aliases-disabled", oldAutoDisabled)
	snapst.AutoAliasesDisabled = true
	t.Set("old-aliases-v2", oldAliases)
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

	autoDisabled := snapst.AutoAliasesDisabled
	oldAliases := snapst.Aliases
	newAliases, err := manualUnalias(oldAliases, alias)
	if err != nil {
		return err
	}

	added, removed, err := applyAliasesChange(snapName, autoDisabled, oldAliases, autoDisabled, newAliases, m.backend, snapst.AliasesPending)
	if err != nil {
		return err
	}
	if err := aliasesTrace(t, added, removed); err != nil {
		return err
	}

	t.Set("old-aliases-v2", oldAliases)
	snapst.Aliases = newAliases
	Set(st, snapName, snapst)
	return nil
}

// otherDisabledAliases is used to track for the benefit of undo what
// changes were made aka what aliases were disabled of another
// conflicting snap by prefer logic
type otherDisabledAliases struct {
	// Auto records whether prefer had to disable automatic aliases
	Auto bool `json:"auto,omitempty"`
	// Manual records which manual aliases were removed by prefer
	Manual map[string]string `json:"manual,omitempty"`
}

func (m *SnapManager) doPreferAliasesV2(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	snapName := snapsup.Name()

	if !snapst.AutoAliasesDisabled {
		// already enabled, nothing to do
		return nil
	}

	curAliases := snapst.Aliases
	aliasConflicts, err := checkAliasesConflicts(st, snapName, false, curAliases, nil)
	conflErr, isConflErr := err.(*AliasConflictError)
	if err != nil && !isConflErr {
		return err
	}
	if isConflErr && conflErr.Conflicts == nil {
		// it's a snap command namespace conflict, we cannot remedy it
		return conflErr
	}
	// proceed to disable conflicting aliases as needed
	// before re-enabling snapName aliases

	otherSnapStates := make(map[string]*SnapState, len(aliasConflicts))
	otherSnapDisabled := make(map[string]*otherDisabledAliases, len(aliasConflicts))
	for otherSnap := range aliasConflicts {
		var otherSnapState SnapState
		err := Get(st, otherSnap, &otherSnapState)
		if err != nil {
			return err
		}

		otherAliases, disabledManual := disableAliases(otherSnapState.Aliases)

		added, removed, err := applyAliasesChange(otherSnap, otherSnapState.AutoAliasesDisabled, otherSnapState.Aliases, true, otherAliases, m.backend, otherSnapState.AliasesPending)
		if err != nil {
			return err
		}
		if err := aliasesTrace(t, added, removed); err != nil {
			return err
		}

		var otherDisabled otherDisabledAliases
		otherDisabled.Manual = disabledManual
		otherSnapState.Aliases = otherAliases
		// disable automatic aliases as needed
		if !otherSnapState.AutoAliasesDisabled && len(otherAliases) != 0 {
			// record that we did disable automatic aliases
			otherDisabled.Auto = true
			otherSnapState.AutoAliasesDisabled = true
		}
		otherSnapDisabled[otherSnap] = &otherDisabled
		otherSnapStates[otherSnap] = &otherSnapState
	}

	added, removed, err := applyAliasesChange(snapName, true, curAliases, false, curAliases, m.backend, snapst.AliasesPending)
	if err != nil {
		return err
	}
	if err := aliasesTrace(t, added, removed); err != nil {
		return err
	}

	for otherSnap, otherSnapState := range otherSnapStates {
		Set(st, otherSnap, otherSnapState)
	}
	if len(otherSnapDisabled) != 0 {
		t.Set("other-disabled-aliases", otherSnapDisabled)
	}
	t.Set("old-auto-aliases-disabled", true)
	t.Set("old-aliases-v2", curAliases)
	snapst.AutoAliasesDisabled = false
	Set(st, snapName, snapst)
	return nil
}
