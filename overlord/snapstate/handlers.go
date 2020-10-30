// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/settings"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timings"
)

// TaskSnapSetup returns the SnapSetup with task params hold by or referred to by the task.
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
	if ts == nil {
		return nil, fmt.Errorf("internal error: tasks are being pruned")
	}
	if err := ts.Get("snap-setup", &snapsup); err != nil {
		return nil, err
	}
	return &snapsup, nil
}

// SetTaskSnapSetup writes the given SnapSetup to the provided task's
// snap-setup-task Task, or to the task itself if the task does not have a
// snap-setup-task (i.e. it _is_ the snap-setup-task)
func SetTaskSnapSetup(t *state.Task, snapsup *SnapSetup) error {
	if t.Has("snap-setup") {
		// this is the snap-setup-task so just write to the task directly
		t.Set("snap-setup", snapsup)
	} else {
		// this task isn't the snap-setup-task, so go get that and write to that
		// one
		var id string
		err := t.Get("snap-setup-task", &id)
		if err != nil {
			return err
		}

		ts := t.State().Task(id)
		if ts == nil {
			return fmt.Errorf("internal error: tasks are being pruned")
		}
		ts.Set("snap-setup", snapsup)
	}

	return nil
}

func snapSetupAndState(t *state.Task) (*SnapSetup, *SnapState, error) {
	snapsup, err := TaskSnapSetup(t)
	if err != nil {
		return nil, nil, err
	}
	var snapst SnapState
	err = Get(t.State(), snapsup.InstanceName(), &snapst)
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

const defaultCoreSnapName = "core"

func defaultBaseSnapsChannel() string {
	channel := os.Getenv("SNAPD_BASES_CHANNEL")
	if channel == "" {
		return "stable"
	}
	return channel
}

func defaultSnapdSnapsChannel() string {
	channel := os.Getenv("SNAPD_SNAPD_CHANNEL")
	if channel == "" {
		return "stable"
	}
	return channel
}

func defaultPrereqSnapsChannel() string {
	channel := os.Getenv("SNAPD_PREREQS_CHANNEL")
	if channel == "" {
		return "stable"
	}
	return channel
}

func linkSnapInFlight(st *state.State, snapName string) (bool, error) {
	for _, chg := range st.Changes() {
		if chg.Status().Ready() {
			continue
		}
		for _, tc := range chg.Tasks() {
			if tc.Status().Ready() {
				continue
			}
			if tc.Kind() == "link-snap" {
				snapsup, err := TaskSnapSetup(tc)
				if err != nil {
					return false, err
				}
				if snapsup.InstanceName() == snapName {
					return true, nil
				}
			}
		}
	}

	return false, nil
}

func isInstalled(st *state.State, snapName string) (bool, error) {
	var snapState SnapState
	err := Get(st, snapName, &snapState)
	if err != nil && err != state.ErrNoState {
		return false, err
	}
	return snapState.IsInstalled(), nil
}

// timeout for tasks to check if the prerequisites are ready
var prerequisitesRetryTimeout = 30 * time.Second

func (m *SnapManager) doPrerequisites(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(t)
	defer perfTimings.Save(st)

	// check if we need to inject tasks to install core
	snapsup, _, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	// os/base/kernel/gadget cannot have prerequisites other
	// than the models default base (or core) which is installed anyway
	switch snapsup.Type {
	case snap.TypeOS, snap.TypeBase, snap.TypeKernel, snap.TypeGadget:
		return nil
	}
	// snapd is special and has no prereqs
	if snapsup.Type == snap.TypeSnapd {
		return nil
	}

	// we need to make sure we install all prereqs together in one
	// operation
	base := defaultCoreSnapName
	if snapsup.Base != "" {
		base = snapsup.Base
	}

	if err := m.installPrereqs(t, base, snapsup.Prereq, snapsup.UserID, perfTimings); err != nil {
		return err
	}

	return nil
}

func (m *SnapManager) installOneBaseOrRequired(st *state.State, snapName string, requireTypeBase bool, channel string, onInFlight error, userID int) (*state.TaskSet, error) {
	// The core snap provides everything we need for core16.
	coreInstalled, err := isInstalled(st, "core")
	if err != nil {
		return nil, err
	}
	if snapName == "core16" && coreInstalled {
		return nil, nil
	}

	// installed already?
	isInstalled, err := isInstalled(st, snapName)
	if err != nil {
		return nil, err
	}
	if isInstalled {
		return nil, nil
	}
	// in progress?
	inFlight, err := linkSnapInFlight(st, snapName)
	if err != nil {
		return nil, err
	}
	if inFlight {
		return nil, onInFlight
	}

	// not installed, nor queued for install -> install it
	ts, err := Install(context.TODO(), st, snapName, &RevisionOptions{Channel: channel}, userID, Flags{RequireTypeBase: requireTypeBase})

	// something might have triggered an explicit install while
	// the state was unlocked -> deal with that here by simply
	// retrying the operation.
	if _, ok := err.(*ChangeConflictError); ok {
		return nil, &state.Retry{After: prerequisitesRetryTimeout}
	}
	return ts, err
}

func (m *SnapManager) installPrereqs(t *state.Task, base string, prereq []string, userID int, tm timings.Measurer) error {
	st := t.State()

	// We try to install all wanted snaps. If one snap cannot be installed
	// because of change conflicts or similar we retry. Only if all snaps
	// can be installed together we add the tasks to the change.
	var tss []*state.TaskSet
	for _, prereqName := range prereq {
		var onInFlightErr error = nil
		var err error
		var ts *state.TaskSet
		timings.Run(tm, "install-prereq", fmt.Sprintf("install %q", prereqName), func(timings.Measurer) {
			noTypeBaseCheck := false
			ts, err = m.installOneBaseOrRequired(st, prereqName, noTypeBaseCheck, defaultPrereqSnapsChannel(), onInFlightErr, userID)
		})
		if err != nil {
			return prereqError("prerequisite", prereqName, err)
		}
		if ts == nil {
			continue
		}
		tss = append(tss, ts)
	}

	// for base snaps we need to wait until the change is done
	// (either finished or failed)
	onInFlightErr := &state.Retry{After: prerequisitesRetryTimeout}

	var tsBase *state.TaskSet
	var err error
	if base != "none" {
		timings.Run(tm, "install-prereq", fmt.Sprintf("install base %q", base), func(timings.Measurer) {
			requireTypeBase := true
			tsBase, err = m.installOneBaseOrRequired(st, base, requireTypeBase, defaultBaseSnapsChannel(), onInFlightErr, userID)
		})
		if err != nil {
			return prereqError("snap base", base, err)
		}
	}

	// on systems without core or snapd need to install snapd to
	// make interfaces work - LP: 1819318
	var tsSnapd *state.TaskSet
	snapdSnapInstalled, err := isInstalled(st, "snapd")
	if err != nil {
		return err
	}
	coreSnapInstalled, err := isInstalled(st, "core")
	if err != nil {
		return err
	}
	if base != "core" && !snapdSnapInstalled && !coreSnapInstalled {
		timings.Run(tm, "install-prereq", "install snapd", func(timings.Measurer) {
			noTypeBaseCheck := false
			tsSnapd, err = m.installOneBaseOrRequired(st, "snapd", noTypeBaseCheck, defaultSnapdSnapsChannel(), onInFlightErr, userID)
		})
		if err != nil {
			return prereqError("system snap", "snapd", err)
		}
	}

	chg := t.Change()
	// add all required snaps, no ordering, this will be done in the
	// auto-connect task handler
	for _, ts := range tss {
		ts.JoinLane(st.NewLane())
		chg.AddAll(ts)
	}
	// add the base if needed, prereqs else must wait on this
	if tsBase != nil {
		tsBase.JoinLane(st.NewLane())
		for _, t := range chg.Tasks() {
			t.WaitAll(tsBase)
		}
		chg.AddAll(tsBase)
	}
	// add snapd if needed, everything must wait on this
	if tsSnapd != nil {
		tsSnapd.JoinLane(st.NewLane())
		for _, t := range chg.Tasks() {
			t.WaitAll(tsSnapd)
		}
		chg.AddAll(tsSnapd)
	}

	// make sure that the new change is committed to the state
	// together with marking this task done
	t.SetStatus(state.DoneStatus)

	return nil
}

func prereqError(what, snapName string, err error) error {
	if _, ok := err.(*state.Retry); ok {
		return err
	}
	return fmt.Errorf("cannot install %s %q: %v", what, snapName, err)
}

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

	// Only report and error if there is an actual error in the change,
	// we could undo things because the user canceled the change.
	var isErr bool
	for _, tt := range t.Change().Tasks() {
		if tt.Status() == state.ErrorStatus {
			isErr = true
			break
		}
	}
	if isErr && !settings.ProblemReportsDisabled(st) {
		st.Unlock()
		oopsid, err := errtrackerReport(snapsup.SideInfo.RealName, strings.Join(logMsg, "\n"), strings.Join(dupSig, "\n"), extra)
		st.Lock()
		if err == nil {
			logger.Noticef("Reported install problem for %q as %s", snapsup.SideInfo.RealName, oopsid)
		} else {
			logger.Debugf("Cannot report problem: %s", err)
		}
	}

	return nil
}

func installInfoUnlocked(st *state.State, snapsup *SnapSetup, deviceCtx DeviceContext) (store.SnapActionResult, error) {
	st.Lock()
	defer st.Unlock()
	opts := &RevisionOptions{Channel: snapsup.Channel, CohortKey: snapsup.CohortKey, Revision: snapsup.Revision()}
	return installInfo(context.TODO(), st, snapsup.InstanceName(), opts, snapsup.UserID, deviceCtx)
}

// autoRefreshRateLimited returns the rate limit of auto-refreshes or 0 if
// there is no limit.
func autoRefreshRateLimited(st *state.State) (rate int64) {
	tr := config.NewTransaction(st)

	var rateLimit string
	err := tr.Get("core", "refresh.rate-limit", &rateLimit)
	if err != nil {
		return 0
	}
	// NOTE ParseByteSize errors on negative rates
	val, err := strutil.ParseByteSize(rateLimit)
	if err != nil {
		return 0
	}
	return val
}

func downloadSnapParams(st *state.State, t *state.Task) (*SnapSetup, StoreService, *auth.UserState, error) {
	snapsup, err := TaskSnapSetup(t)
	if err != nil {
		return nil, nil, nil, err
	}

	deviceCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return nil, nil, nil, err
	}

	sto := Store(st, deviceCtx)

	user, err := userFromUserID(st, snapsup.UserID)
	if err != nil {
		return nil, nil, nil, err
	}

	return snapsup, sto, user, nil
}

func (m *SnapManager) doDownloadSnap(t *state.Task, tomb *tomb.Tomb) error {
	st := t.State()
	var rate int64

	st.Lock()
	perfTimings := state.TimingsForTask(t)
	snapsup, theStore, user, err := downloadSnapParams(st, t)
	if snapsup != nil && snapsup.IsAutoRefresh {
		// NOTE rate is never negative
		rate = autoRefreshRateLimited(st)
	}
	st.Unlock()
	if err != nil {
		return err
	}

	meter := NewTaskProgressAdapterUnlocked(t)
	targetFn := snapsup.MountFile()

	dlOpts := &store.DownloadOptions{
		IsAutoRefresh: snapsup.IsAutoRefresh,
		RateLimit:     rate,
	}
	if snapsup.DownloadInfo == nil {
		var storeInfo store.SnapActionResult
		// COMPATIBILITY - this task was created from an older version
		// of snapd that did not store the DownloadInfo in the state
		// yet. Therefore do not worry about DeviceContext.
		storeInfo, err = installInfoUnlocked(st, snapsup, nil)
		if err != nil {
			return err
		}
		timings.Run(perfTimings, "download", fmt.Sprintf("download snap %q", snapsup.SnapName()), func(timings.Measurer) {
			err = theStore.Download(tomb.Context(nil), snapsup.SnapName(), targetFn, &storeInfo.DownloadInfo, meter, user, dlOpts)
		})
		snapsup.SideInfo = &storeInfo.SideInfo
	} else {
		timings.Run(perfTimings, "download", fmt.Sprintf("download snap %q", snapsup.SnapName()), func(timings.Measurer) {
			err = theStore.Download(tomb.Context(nil), snapsup.SnapName(), targetFn, snapsup.DownloadInfo, meter, user, dlOpts)
		})
	}
	if err != nil {
		return err
	}

	snapsup.SnapPath = targetFn

	// update the snap setup for the follow up tasks
	st.Lock()
	t.Set("snap-setup", snapsup)
	perfTimings.Save(st)
	st.Unlock()

	return nil
}

var (
	mountPollInterval = 1 * time.Second
)

// hasOtherInstances checks whether there are other instances of the snap, be it
// instance keyed or not
func hasOtherInstances(st *state.State, instanceName string) (bool, error) {
	snapName, _ := snap.SplitInstanceName(instanceName)
	var all map[string]*json.RawMessage
	if err := st.Get("snaps", &all); err != nil && err != state.ErrNoState {
		return false, err
	}
	for otherName := range all {
		if otherName == instanceName {
			continue
		}
		if otherSnapName, _ := snap.SplitInstanceName(otherName); otherSnapName == snapName {
			return true, nil
		}
	}
	return false, nil
}

func (m *SnapManager) doMountSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	perfTimings := state.TimingsForTask(t)
	snapsup, snapst, err := snapSetupAndState(t)
	st.Unlock()
	if err != nil {
		return err
	}

	curInfo, err := snapst.CurrentInfo()
	if err != nil && err != ErrNoCurrent {
		return err
	}

	m.backend.CurrentInfo(curInfo)

	st.Lock()
	deviceCtx, err := DeviceCtx(t.State(), t, nil)
	st.Unlock()
	if err != nil {
		return err
	}

	timings.Run(perfTimings, "check-snap", fmt.Sprintf("check snap %q", snapsup.InstanceName()), func(timings.Measurer) {
		err = checkSnap(st, snapsup.SnapPath, snapsup.InstanceName(), snapsup.SideInfo, curInfo, snapsup.Flags, deviceCtx)
	})
	if err != nil {
		return err
	}

	cleanup := func() {
		st.Lock()
		defer st.Unlock()

		otherInstances, err := hasOtherInstances(st, snapsup.InstanceName())
		if err != nil {
			t.Errorf("cannot cleanup partial setup snap %q: %v", snapsup.InstanceName(), err)
			return
		}

		// remove snap dir is idempotent so it's ok to always call it in the cleanup path
		if err := m.backend.RemoveSnapDir(snapsup.placeInfo(), otherInstances); err != nil {
			t.Errorf("cannot cleanup partial setup snap %q: %v", snapsup.InstanceName(), err)
		}

	}

	pb := NewTaskProgressAdapterUnlocked(t)
	// TODO Use snapsup.Revision() to obtain the right info to mount
	//      instead of assuming the candidate is the right one.
	var snapType snap.Type
	var installRecord *backend.InstallRecord
	timings.Run(perfTimings, "setup-snap", fmt.Sprintf("setup snap %q", snapsup.InstanceName()), func(timings.Measurer) {
		snapType, installRecord, err = m.backend.SetupSnap(snapsup.SnapPath, snapsup.InstanceName(), snapsup.SideInfo, deviceCtx, pb)
	})
	if err != nil {
		cleanup()
		return err
	}

	// double check that the snap is mounted
	var readInfoErr error
	for i := 0; i < 10; i++ {
		_, readInfoErr = readInfo(snapsup.InstanceName(), snapsup.SideInfo, errorOnBroken)
		if readInfoErr == nil {
			break
		}
		if _, ok := readInfoErr.(*snap.NotFoundError); !ok {
			break
		}
		// snap not found, seems is not mounted yet
		msg := fmt.Sprintf("expected snap %q revision %v to be mounted but is not", snapsup.InstanceName(), snapsup.Revision())
		readInfoErr = fmt.Errorf("cannot proceed, %s", msg)
		if i == 0 {
			logger.Noticef(msg)
		}
		time.Sleep(mountPollInterval)
	}
	if readInfoErr != nil {
		timings.Run(perfTimings, "undo-setup-snap", fmt.Sprintf("Undo setup of snap %q", snapsup.InstanceName()), func(timings.Measurer) {
			err = m.backend.UndoSetupSnap(snapsup.placeInfo(), snapType, installRecord, deviceCtx, pb)
		})
		if err != nil {
			st.Lock()
			t.Errorf("cannot undo partial setup snap %q: %v", snapsup.InstanceName(), err)
			st.Unlock()
		}

		cleanup()
		return readInfoErr
	}

	st.Lock()
	// set snapst type for undoMountSnap
	t.Set("snap-type", snapType)
	if installRecord != nil {
		t.Set("install-record", installRecord)
	}
	st.Unlock()

	if snapsup.Flags.RemoveSnapPath {
		if err := os.Remove(snapsup.SnapPath); err != nil {
			logger.Noticef("Failed to cleanup %s: %s", snapsup.SnapPath, err)
		}
	}

	st.Lock()
	perfTimings.Save(st)
	st.Unlock()

	return nil
}

func (m *SnapManager) undoMountSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	snapsup, err := TaskSnapSetup(t)
	st.Unlock()
	if err != nil {
		return err
	}

	st.Lock()
	deviceCtx, err := DeviceCtx(t.State(), t, nil)
	st.Unlock()
	if err != nil {
		return err
	}

	st.Lock()
	var typ snap.Type
	err = t.Get("snap-type", &typ)
	st.Unlock()
	// backward compatibility
	if err == state.ErrNoState {
		typ = "app"
	} else if err != nil {
		return err
	}

	var installRecord backend.InstallRecord
	st.Lock()
	// install-record is optional (e.g. not present in tasks from older snapd)
	err = t.Get("install-record", &installRecord)
	st.Unlock()
	if err != nil && err != state.ErrNoState {
		return err
	}

	pb := NewTaskProgressAdapterUnlocked(t)
	if err := m.backend.UndoSetupSnap(snapsup.placeInfo(), typ, &installRecord, deviceCtx, pb); err != nil {
		return err
	}

	st.Lock()
	defer st.Unlock()

	otherInstances, err := hasOtherInstances(st, snapsup.InstanceName())
	if err != nil {
		return err
	}

	return m.backend.RemoveSnapDir(snapsup.placeInfo(), otherInstances)
}

// queryDisabledServices uses wrappers.QueryDisabledServices()
//
// Note this function takes a snap info rather than snapst because there are
// situations where we want to call this on non-current snap infos, i.e. in the
// undo handlers, see undoLinkSnap for an example.
func (m *SnapManager) queryDisabledServices(info *snap.Info, pb progress.Meter) ([]string, error) {
	return m.backend.QueryDisabledServices(info, pb)
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

	// add to the disabled services list in snapst services which were disabled
	// when stop-snap-services ran, for usage across changes like in reverting
	// and enabling after being disabled.
	// we keep what's already in the list in snapst because that list is
	// services which were previously present in the snap and disabled, but are
	// no longer present.
	snapst.LastActiveDisabledServices = append(
		snapst.LastActiveDisabledServices,
		snapsup.LastActiveDisabledServices...,
	)

	tr := config.NewTransaction(st)
	experimentalRefreshAppAwareness, err := features.Flag(tr, features.RefreshAppAwareness)
	if err != nil && !config.IsNoOption(err) {
		return err
	}

	if experimentalRefreshAppAwareness && !snapsup.Flags.IgnoreRunning {
		// Invoke the hard refresh flow. Upon success the returned lock will be
		// held to prevent snap-run from advancing until UnlinkSnap, executed
		// below, completes.
		lock, err := hardEnsureNothingRunningDuringRefresh(m.backend, st, snapst, oldInfo)
		if err != nil {
			return err
		}
		defer lock.Close()
	}

	snapst.Active = false

	// do the final unlink
	linkCtx := backend.LinkContext{
		FirstInstall: false,
		// This task is only used for unlinking a snap during refreshes so we
		// can safely hard-code this condition here.
		RunInhibitHint: runinhibit.HintInhibitedForRefresh,
	}
	err = m.backend.UnlinkSnap(oldInfo, linkCtx, NewTaskProgressAdapterLocked(t))
	if err != nil {
		return err
	}

	// mark as inactive
	Set(st, snapsup.InstanceName(), snapst)

	// Notify link snap participants about link changes.
	notifyLinkParticipants(t, snapsup.InstanceName())
	return nil
}

func (m *SnapManager) undoUnlinkCurrentSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(t)
	defer perfTimings.Save(st)

	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	oldInfo, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}

	deviceCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return err
	}

	// get the services which LinkSnap should disable when generating wrappers,
	// as well as the services which are not present in this revision, but were
	// present and disabled in a previous one and as such should be kept inside
	// snapst for persistent storage.
	svcsToSave, svcsToDisable, err := missingDisabledServices(snapst.LastActiveDisabledServices, oldInfo)
	if err != nil {
		return err
	}

	snapst.Active = true
	vitalityRank, err := vitalityRank(st, snapsup.InstanceName())
	if err != nil {
		return err
	}
	linkCtx := backend.LinkContext{
		PrevDisabledServices: svcsToDisable,
		FirstInstall:         false,
		VitalityRank:         vitalityRank,
	}
	reboot, err := m.backend.LinkSnap(oldInfo, deviceCtx, linkCtx, perfTimings)
	if err != nil {
		return err
	}

	// re-save the missing services so when we unlink this revision and go to a
	// different revision with potentially different service names, the
	// currently missing service names will be re-disabled if they exist later
	snapst.LastActiveDisabledServices = svcsToSave

	// mark as active again
	Set(st, snapsup.InstanceName(), snapst)

	// Notify link snap participants about link changes.
	notifyLinkParticipants(t, snapsup.InstanceName())

	// if we just put back a previous a core snap, request a restart
	// so that we switch executing its snapd
	m.maybeRestart(t, oldInfo, reboot, deviceCtx)
	return nil
}

func (m *SnapManager) doCopySnapData(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	snapsup, snapst, err := snapSetupAndState(t)
	st.Unlock()
	if err != nil {
		return err
	}

	newInfo, err := readInfo(snapsup.InstanceName(), snapsup.SideInfo, 0)
	if err != nil {
		return err
	}

	oldInfo, err := snapst.CurrentInfo()
	if err != nil && err != ErrNoCurrent {
		return err
	}

	pb := NewTaskProgressAdapterUnlocked(t)
	if copyDataErr := m.backend.CopySnapData(newInfo, oldInfo, pb); copyDataErr != nil {
		if oldInfo != nil {
			// there is another revision of the snap, cannot remove
			// shared data directory
			return copyDataErr
		}

		// cleanup shared snap data directory
		st.Lock()
		defer st.Unlock()

		otherInstances, err := hasOtherInstances(st, snapsup.InstanceName())
		if err != nil {
			t.Errorf("cannot undo partial snap %q data copy: %v", snapsup.InstanceName(), err)
			return copyDataErr
		}
		// no other instances of this snap, shared data directory can be
		// removed now too
		if err := m.backend.RemoveSnapDataDir(newInfo, otherInstances); err != nil {
			t.Errorf("cannot undo partial snap %q data copy, failed removing shared directory: %v", snapsup.InstanceName(), err)
		}
		return copyDataErr
	}
	return nil
}

func (m *SnapManager) undoCopySnapData(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	snapsup, snapst, err := snapSetupAndState(t)
	st.Unlock()
	if err != nil {
		return err
	}

	newInfo, err := readInfo(snapsup.InstanceName(), snapsup.SideInfo, 0)
	if err != nil {
		return err
	}

	oldInfo, err := snapst.CurrentInfo()
	if err != nil && err != ErrNoCurrent {
		return err
	}

	pb := NewTaskProgressAdapterUnlocked(t)
	if err := m.backend.UndoCopySnapData(newInfo, oldInfo, pb); err != nil {
		return err
	}

	if oldInfo != nil {
		// there is other revision of this snap, cannot remove shared
		// directory anyway
		return nil
	}

	st.Lock()
	defer st.Unlock()

	otherInstances, err := hasOtherInstances(st, snapsup.InstanceName())
	if err != nil {
		return err
	}
	// no other instances of this snap and no other revisions, shared data
	// directory can be removed
	if err := m.backend.RemoveSnapDataDir(newInfo, otherInstances); err != nil {
		return err
	}
	return nil
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

// writeSeqFile writes the sequence file for failover handling
func writeSeqFile(name string, snapst *SnapState) error {
	p := filepath.Join(dirs.SnapSeqDir, name+".json")
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}

	b, err := json.Marshal(&struct {
		Sequence []*snap.SideInfo `json:"sequence"`
		Current  string           `json:"current"`
	}{
		Sequence: snapst.Sequence,
		Current:  snapst.Current.String(),
	})
	if err != nil {
		return err
	}

	return osutil.AtomicWriteFile(p, b, 0644, 0)
}

// missingDisabledServices returns a list of services that were disabled
// that are currently missing from the specific snap info (i.e. they were
// renamed in this snap info), as well as a list of disabled services that are
// present in this snap info.
// the first arg is the disabled services when the snap was last active
func missingDisabledServices(svcs []string, info *snap.Info) ([]string, []string, error) {
	// make a copy of all the previously disabled services that we will remove
	// from, as well as an empty list to add to for the found services
	missingSvcs := []string{}
	foundSvcs := []string{}

	// for all the previously disabled services, check if they are in the
	// current snap info revision as services or not
	for _, disabledSvcName := range svcs {
		// check if the service is an app _and_ is a service
		if app, ok := info.Apps[disabledSvcName]; ok && app.IsService() {
			foundSvcs = append(foundSvcs, disabledSvcName)
		} else {
			missingSvcs = append(missingSvcs, disabledSvcName)
		}
	}

	// sort the lists for easier testing
	sort.Strings(missingSvcs)
	sort.Strings(foundSvcs)

	return missingSvcs, foundSvcs, nil
}

func vitalityRank(st *state.State, instanceName string) (rank int, err error) {
	tr := config.NewTransaction(st)

	var vitalityStr string
	err = tr.GetMaybe("core", "resilience.vitality-hint", &vitalityStr)
	if err != nil {
		return 0, err
	}
	for i, s := range strings.Split(vitalityStr, ",") {
		if s == instanceName {
			return i + 1, nil
		}
	}
	return 0, nil
}

// LinkSnapParticipant is an interface for interacting with snap link/unlink
// operations.
//
// Unlike the interface for a task handler, only one notification method is
// used. The method notifies a participant that linkage of a snap has changed.
// This method is invoked in link-snap, unlink-snap, the undo path of those
// methods and the undo handler for link-snap.
//
// In all cases it is invoked after all other operations are completed but
// before the task completes.
type LinkSnapParticipant interface {
	// SnapLinkageChanged is called when a snap is linked or unlinked.
	// The error is only logged and does not stop the task it is used from.
	SnapLinkageChanged(st *state.State, instanceName string) error
}

var linkSnapParticipants []LinkSnapParticipant

// AddLinkSnapParticipant adds a participant in the link/unlink operations.
func AddLinkSnapParticipant(p LinkSnapParticipant) {
	linkSnapParticipants = append(linkSnapParticipants, p)
}

// MockLinkSnapParticipants replaces the list of link snap participants for testing.
func MockLinkSnapParticipants(ps []LinkSnapParticipant) (restore func()) {
	old := linkSnapParticipants
	linkSnapParticipants = ps
	return func() {
		linkSnapParticipants = old
	}
}

func notifyLinkParticipants(t *state.Task, instanceName string) {
	st := t.State()
	for _, p := range linkSnapParticipants {
		if err := p.SnapLinkageChanged(st, instanceName); err != nil {
			t.Errorf("%v", err)
		}
	}
}

func (m *SnapManager) doLinkSnap(t *state.Task, _ *tomb.Tomb) (err error) {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(t)
	defer perfTimings.Save(st)

	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	deviceCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return err
	}

	// find if the snap is already installed before we modify snapst below
	isInstalled := snapst.IsInstalled()

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
	oldChannel := snapst.TrackingChannel
	if snapsup.Channel != "" {
		err := snapst.SetTrackingChannel(snapsup.Channel)
		if err != nil {
			return err
		}
	}
	oldIgnoreValidation := snapst.IgnoreValidation
	snapst.IgnoreValidation = snapsup.IgnoreValidation
	oldTryMode := snapst.TryMode
	snapst.TryMode = snapsup.TryMode
	oldDevMode := snapst.DevMode
	snapst.DevMode = snapsup.DevMode
	oldJailMode := snapst.JailMode
	snapst.JailMode = snapsup.JailMode
	oldClassic := snapst.Classic
	snapst.Classic = snapsup.Classic
	oldCohortKey := snapst.CohortKey
	snapst.CohortKey = snapsup.CohortKey
	if snapsup.Required { // set only on install and left alone on refresh
		snapst.Required = true
	}
	oldRefreshInhibitedTime := snapst.RefreshInhibitedTime
	// only set userID if unset or logged out in snapst and if we
	// actually have an associated user
	if snapsup.UserID > 0 {
		var user *auth.UserState
		if snapst.UserID != 0 {
			user, err = auth.User(st, snapst.UserID)
			if err != nil && err != auth.ErrInvalidUser {
				return err
			}
		}
		if user == nil {
			// if the original user installing the snap is
			// no longer available transfer to user who
			// triggered this change
			snapst.UserID = snapsup.UserID
		}
	}
	// keep instance key
	snapst.InstanceKey = snapsup.InstanceKey

	newInfo, err := readInfo(snapsup.InstanceName(), cand, 0)
	if err != nil {
		return err
	}

	// record type
	snapst.SetType(newInfo.Type())

	pb := NewTaskProgressAdapterLocked(t)

	// Check for D-Bus service conflicts a second time to detect
	// conflicts within a transaction.
	if err := checkDBusServiceConflicts(st, newInfo); err != nil {
		return err
	}

	// get the services which LinkSnap should disable when generating wrappers,
	// as well as the services which are not present in this revision, but were
	// present and disabled in a previous one and as such should be kept inside
	// snapst for persistent storage
	svcsToSave, svcsToDisable, err := missingDisabledServices(snapst.LastActiveDisabledServices, newInfo)
	if err != nil {
		return err
	}

	vitalityRank, err := vitalityRank(st, snapsup.InstanceName())
	if err != nil {
		return err
	}
	linkCtx := backend.LinkContext{
		FirstInstall:         oldCurrent.Unset(),
		PrevDisabledServices: svcsToDisable,
		VitalityRank:         vitalityRank,
	}
	reboot, err := m.backend.LinkSnap(newInfo, deviceCtx, linkCtx, perfTimings)
	// defer a cleanup helper which will unlink the snap if anything fails after
	// this point
	defer func() {
		if err == nil {
			return
		}
		// err is not nil, we need to try and unlink the snap to cleanup after
		// ourselves
		var unlinkErr error
		unlinkErr = m.backend.UnlinkSnap(newInfo, linkCtx, pb)
		if unlinkErr != nil {
			t.Errorf("cannot cleanup failed attempt at making snap %q available to the system: %v", snapsup.InstanceName(), unlinkErr)
		}
		notifyLinkParticipants(t, snapsup.InstanceName())
	}()
	if err != nil {
		return err
	}

	// commit the missing services to state so when we unlink this revision and
	// go to a different revision with potentially different service names, the
	// currently missing service names will be re-disabled if they exist later
	snapst.LastActiveDisabledServices = svcsToSave

	// Restore configuration of the target revision (if available) on revert
	if isInstalled {
		// Make a copy of configuration of current snap revision
		if err = config.SaveRevisionConfig(st, snapsup.InstanceName(), oldCurrent); err != nil {
			return err
		}
	}

	// Restore configuration of the target revision (if available; nothing happens if it's not).
	// We only do this on reverts (and not on refreshes).
	if snapsup.Revert {
		if err = config.RestoreRevisionConfig(st, snapsup.InstanceName(), snapsup.Revision()); err != nil {
			return err
		}
	}

	if len(snapst.Sequence) == 1 {
		if err := m.createSnapCookie(st, snapsup.InstanceName()); err != nil {
			return fmt.Errorf("cannot create snap cookie: %v", err)
		}
	}
	// save for undoLinkSnap
	t.Set("old-trymode", oldTryMode)
	t.Set("old-devmode", oldDevMode)
	t.Set("old-jailmode", oldJailMode)
	t.Set("old-classic", oldClassic)
	t.Set("old-last-active-disabled-services", svcsToSave)
	t.Set("old-ignore-validation", oldIgnoreValidation)
	t.Set("old-channel", oldChannel)
	t.Set("old-current", oldCurrent)
	t.Set("old-candidate-index", oldCandidateIndex)
	t.Set("old-refresh-inhibited-time", oldRefreshInhibitedTime)
	t.Set("old-cohort-key", oldCohortKey)

	// Record the fact that the snap was refreshed successfully.
	snapst.RefreshInhibitedTime = nil

	if cand.SnapID != "" {
		// write the auxiliary store info
		aux := &auxStoreInfo{
			Media:   snapsup.Media,
			Website: snapsup.Website,
		}
		if err := keepAuxStoreInfo(cand.SnapID, aux); err != nil {
			return err
		}
		if len(snapst.Sequence) == 1 {
			defer func() {
				if err != nil {
					// the install is getting undone, and there are no more of this snap
					// try to remove the aux info we just created
					discardAuxStoreInfo(cand.SnapID)
				}
			}()
		}
	}

	// write sequence file for failover helpers
	if err := writeSeqFile(snapsup.InstanceName(), snapst); err != nil {
		return err
	}

	// Compatibility with old snapd: check if we have auto-connect task and
	// if not, inject it after self (link-snap) for snaps that are not core
	if newInfo.Type() != snap.TypeOS {
		var hasAutoConnect, hasSetupProfiles bool
		for _, other := range t.Change().Tasks() {
			// Check if this is auto-connect task for same snap and we it's part of the change with setup-profiles task
			if other.Kind() == "auto-connect" || other.Kind() == "setup-profiles" {
				otherSnapsup, err := TaskSnapSetup(other)
				if err != nil {
					return err
				}
				if snapsup.InstanceName() == otherSnapsup.InstanceName() {
					if other.Kind() == "auto-connect" {
						hasAutoConnect = true
					} else {
						hasSetupProfiles = true
					}
				}
			}
		}
		if !hasAutoConnect && hasSetupProfiles {
			InjectAutoConnect(t, snapsup)
		}
	}

	// Do at the end so we only preserve the new state if it worked.
	Set(st, snapsup.InstanceName(), snapst)

	// Notify link snap participants about link changes.
	notifyLinkParticipants(t, snapsup.InstanceName())

	// Make sure if state commits and snapst is mutated we won't be rerun
	t.SetStatus(state.DoneStatus)

	// if we just installed a core snap, request a restart
	// so that we switch executing its snapd.
	m.maybeRestart(t, newInfo, reboot, deviceCtx)

	return nil
}

// maybeRestart will schedule a reboot or restart as needed for the
// just linked snap with info if it's a core or snapd or kernel snap.
func (m *SnapManager) maybeRestart(t *state.Task, info *snap.Info, rebootRequired bool, deviceCtx DeviceContext) {
	// Don't restart when preseeding - we will switch to new snapd on
	// first boot.
	if m.preseed {
		return
	}

	st := t.State()

	if rebootRequired {
		t.Logf("Requested system restart.")
		st.RequestRestart(state.RestartSystem)
		return
	}

	typ := info.Type()

	// if bp is non-trivial then either we're not on classic, or the snap is
	// snapd. So daemonRestartReason will always return "" which is what we
	// want. If that combination stops being true and there's a situation
	// where a non-trivial bp could return a non-empty reason, use IsTrivial
	// to check and bail before reaching this far.

	restartReason := daemonRestartReason(st, typ)
	if restartReason == "" {
		// no message -> no restart
		return
	}

	t.Logf(restartReason)
	st.RequestRestart(state.RestartDaemon)
}

func daemonRestartReason(st *state.State, typ snap.Type) string {
	if !((release.OnClassic && typ == snap.TypeOS) || typ == snap.TypeSnapd) {
		// not interesting
		return ""
	}

	if typ == snap.TypeOS {
		// ignore error here as we have no way to return to caller
		snapdSnapInstalled, _ := isInstalled(st, "snapd")
		if snapdSnapInstalled {
			// this snap is the base, but snapd is running from the snapd snap
			return ""
		}
		return "Requested daemon restart."
	}

	return "Requested daemon restart (snapd snap)."
}

// maybeUndoRemodelBootChanges will check if an undo needs to update the
// bootloader. This can happen if e.g. a new kernel gets installed. This
// will switch the bootloader to the new kernel but if the change is later
// undone we need to switch back to the kernel of the old model.
func (m *SnapManager) maybeUndoRemodelBootChanges(t *state.Task) error {
	// get the new and the old model
	deviceCtx, err := DeviceCtx(t.State(), t, nil)
	if err != nil {
		return err
	}
	// we only have an old model if we are in a remodel situation
	if !deviceCtx.ForRemodeling() {
		return nil
	}
	groundDeviceCtx := deviceCtx.GroundContext()
	oldModel := groundDeviceCtx.Model()
	newModel := deviceCtx.Model()

	// check type of the snap we are undoing, only kernel/base/core are
	// relevant
	snapsup, _, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	var newSnapName, snapName string
	switch snapsup.Type {
	case snap.TypeKernel:
		snapName = oldModel.Kernel()
		newSnapName = newModel.Kernel()
	case snap.TypeOS, snap.TypeBase:
		// XXX: add support for "core"
		snapName = oldModel.Base()
		newSnapName = newModel.Base()
	default:
		return nil
	}
	// we can stop if the kernel/base has not changed
	if snapName == newSnapName {
		return nil
	}
	// we can stop if the snap we are looking at is not a kernel/base
	// of the new model
	if snapsup.InstanceName() != newSnapName {
		return nil
	}
	// get info for *old* kernel/base/core and see if we need to reboot
	// TODO: we may need something like infoForDeviceSnap here
	var snapst SnapState
	if err = Get(t.State(), snapName, &snapst); err != nil {
		return err
	}
	info, err := snapst.CurrentInfo()
	if err != nil && err != ErrNoCurrent {
		return err
	}
	bp := boot.Participant(info, info.Type(), groundDeviceCtx)
	reboot, err := bp.SetNextBoot()
	if err != nil {
		return err
	}

	// we may just have switch back to the old kernel/base/core so
	// we may need to restart
	m.maybeRestart(t, info, reboot, groundDeviceCtx)

	return nil
}

func (m *SnapManager) undoLinkSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(t)
	defer perfTimings.Save(st)

	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	var oldChannel string
	err = t.Get("old-channel", &oldChannel)
	if err != nil {
		return err
	}
	var oldIgnoreValidation bool
	err = t.Get("old-ignore-validation", &oldIgnoreValidation)
	if err != nil && err != state.ErrNoState {
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
	var oldRefreshInhibitedTime *time.Time
	if err := t.Get("old-refresh-inhibited-time", &oldRefreshInhibitedTime); err != nil && err != state.ErrNoState {
		return err
	}
	var oldCohortKey string
	if err := t.Get("old-cohort-key", &oldCohortKey); err != nil && err != state.ErrNoState {
		return err
	}

	var oldLastActiveDisabledServices []string
	if err := t.Get("old-last-active-disabled-services", &oldLastActiveDisabledServices); err != nil && err != state.ErrNoState {
		return err
	}

	if len(snapst.Sequence) == 1 {
		// XXX: shouldn't these two just log and carry on? this is an undo handler...
		timings.Run(perfTimings, "discard-snap-namespace", fmt.Sprintf("discard the namespace of snap %q", snapsup.InstanceName()), func(tm timings.Measurer) {
			err = m.backend.DiscardSnapNamespace(snapsup.InstanceName())
		})
		if err != nil {
			t.Errorf("cannot discard snap namespace %q, will retry in 3 mins: %s", snapsup.InstanceName(), err)
			return &state.Retry{After: 3 * time.Minute}
		}
		if err := m.removeSnapCookie(st, snapsup.InstanceName()); err != nil {
			return fmt.Errorf("cannot remove snap cookie: %v", err)
		}
		// try to remove the auxiliary store info
		if err := discardAuxStoreInfo(snapsup.SideInfo.SnapID); err != nil {
			return fmt.Errorf("cannot remove auxiliary store info: %v", err)
		}
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
	snapst.TrackingChannel = oldChannel
	snapst.IgnoreValidation = oldIgnoreValidation
	snapst.TryMode = oldTryMode
	snapst.DevMode = oldDevMode
	snapst.JailMode = oldJailMode
	snapst.Classic = oldClassic
	snapst.RefreshInhibitedTime = oldRefreshInhibitedTime
	snapst.CohortKey = oldCohortKey
	snapst.LastActiveDisabledServices = oldLastActiveDisabledServices

	newInfo, err := readInfo(snapsup.InstanceName(), snapsup.SideInfo, 0)
	if err != nil {
		return err
	}

	// we need to undo potential changes to current snap configuration (e.g. if
	// modified by post-refresh/install/configure hooks as part of failed
	// refresh/install) by restoring the configuration of "old current".
	// similarly, we need to re-save the disabled services if there is a
	// revision for us to go back to, see comment below for full explanation
	if len(snapst.Sequence) > 0 {
		if err = config.RestoreRevisionConfig(st, snapsup.InstanceName(), oldCurrent); err != nil {
			return err
		}

		// unlock state while we talk to systemd
		st.Unlock()
		defer st.Lock()

		// get the currently disabled services and add them to
		// snapst.LastActiveDisabledServices because if we completed a successful
		// doLinkSnap (hence we are in the undo handler), then we already disabled
		// the services and deleted currently existing services from the state
		// during doLinkSnap, but now we will need that information again when we go
		// to link the old version to prevent accidental enabling of disabled
		// services on a failed revert/refresh
		disabledServices, err := m.queryDisabledServices(newInfo, NewTaskProgressAdapterUnlocked(t))
		if err != nil {
			return err
		}

		st.Lock()
		defer st.Unlock()

		snapst.LastActiveDisabledServices = append(
			snapst.LastActiveDisabledServices,
			disabledServices...,
		)
	} else {
		// in the case of an install we need to clear any config
		err = config.DeleteSnapConfig(st, snapsup.InstanceName())
		if err != nil {
			return err
		}
	}

	pb := NewTaskProgressAdapterLocked(t)
	linkCtx := backend.LinkContext{
		FirstInstall: oldCurrent.Unset(),
	}
	err = m.backend.UnlinkSnap(newInfo, linkCtx, pb)
	if err != nil {
		return err
	}

	if err := m.maybeUndoRemodelBootChanges(t); err != nil {
		return err
	}

	// restart only when snapd was installed for the first time and the rest of
	// the cleanup is performed by snapd from core;
	// when reverting a subsequent snapd revision, the restart happens in
	// undoLinkCurrentSnap() instead
	if linkCtx.FirstInstall && newInfo.Type() == snap.TypeSnapd {
		// only way to get
		deviceCtx, err := DeviceCtx(st, t, nil)
		if err != nil {
			return err
		}
		const rebootRequired = false
		m.maybeRestart(t, newInfo, rebootRequired, deviceCtx)
	}

	// write sequence file for failover helpers
	if err := writeSeqFile(snapsup.InstanceName(), snapst); err != nil {
		return err
	}
	// mark as inactive
	Set(st, snapsup.InstanceName(), snapst)

	// Notify link snap participants about link changes.
	notifyLinkParticipants(t, snapsup.InstanceName())

	// Make sure if state commits and snapst is mutated we won't be rerun
	t.SetStatus(state.UndoneStatus)

	// If we are on classic and have no previous version of core
	// we may have restarted from a distro package into the core
	// snap. We need to undo that restart here. Instead of in
	// doUnlinkCurrentSnap() like we usually do when going from
	// core snap -> next core snap
	if release.OnClassic && newInfo.Type() == snap.TypeOS && oldCurrent.Unset() {
		t.Logf("Requested daemon restart (undo classic initial core install)")
		st.RequestRestart(state.RestartDaemon)
	}
	return nil
}

type doSwitchFlags struct {
	switchCurrentChannel bool
}

// doSwitchSnapChannel switches the snap's tracking channel and/or cohort. It
// also switches the current channel if appropriate. For use from 'Update'.
func (m *SnapManager) doSwitchSnapChannel(t *state.Task, _ *tomb.Tomb) error {
	return m.genericDoSwitchSnap(t, doSwitchFlags{switchCurrentChannel: true})
}

// doSwitchSnap switches the snap's tracking channel and/or cohort, *without*
// switching the current snap channel. For use from 'Switch'.
func (m *SnapManager) doSwitchSnap(t *state.Task, _ *tomb.Tomb) error {
	return m.genericDoSwitchSnap(t, doSwitchFlags{})
}

func (m *SnapManager) genericDoSwitchSnap(t *state.Task, flags doSwitchFlags) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	// switched the tracked channel
	if err := snapst.SetTrackingChannel(snapsup.Channel); err != nil {
		return err
	}
	snapst.CohortKey = snapsup.CohortKey
	if flags.switchCurrentChannel {
		// optionally support switching the current snap channel too, e.g.
		// if a snap is in both stable and candidate with the same revision
		// we can update it here and it will be displayed correctly in the UI
		if snapsup.SideInfo.Channel != "" {
			snapst.CurrentSideInfo().Channel = snapsup.Channel
		}
	}

	Set(st, snapsup.InstanceName(), snapst)
	return nil
}

func (m *SnapManager) doToggleSnapFlags(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	// for now we support toggling only ignore-validation
	snapst.IgnoreValidation = snapsup.IgnoreValidation

	Set(st, snapsup.InstanceName(), snapst)
	return nil
}

func (m *SnapManager) startSnapServices(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(t)
	defer perfTimings.Save(st)

	_, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	currentInfo, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}
	svcs := currentInfo.Services()
	if len(svcs) == 0 {
		return nil
	}

	startupOrdered, err := snap.SortServices(svcs)
	if err != nil {
		return err
	}

	pb := NewTaskProgressAdapterUnlocked(t)
	st.Unlock()
	err = m.backend.StartServices(startupOrdered, pb, perfTimings)
	st.Lock()
	return err
}

func (m *SnapManager) stopSnapServices(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(t)
	defer perfTimings.Save(st)

	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	currentInfo, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}
	svcs := currentInfo.Services()
	if len(svcs) == 0 {
		return nil
	}

	var stopReason snap.ServiceStopReason
	if err := t.Get("stop-reason", &stopReason); err != nil && err != state.ErrNoState {
		return err
	}

	pb := NewTaskProgressAdapterUnlocked(t)
	st.Unlock()
	defer st.Lock()

	// stop the services
	err = m.backend.StopServices(svcs, stopReason, pb, perfTimings)
	if err != nil {
		return err
	}

	// get the disabled services after we stopped all the services.
	// NOTE: we could probably do this before we stopped all the services (or
	// later in a different task from this entirely), but the important ordering
	// for saving the disabled services is that we save the list before we
	// unlink the snap (and hence destroy systemd's state of what services are
	// disabled).
	// this list is not meant to save what services are disabled at any given
	// time, specifically just what services are disabled while systemd loses
	// track of the services because we need to delete and re-generate the
	// service units.
	disabledServices, err := m.queryDisabledServices(currentInfo, pb)
	if err != nil {
		return err
	}

	st.Lock()
	defer st.Unlock()

	// finally commit the disabled services to snapsetup
	snapsup.LastActiveDisabledServices = disabledServices

	err = SetTaskSnapSetup(t, snapsup)
	if err != nil {
		return err
	}

	return nil
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

	info, err := Info(t.State(), snapsup.InstanceName(), snapsup.Revision())
	if err != nil {
		return err
	}

	// do the final unlink
	linkCtx := backend.LinkContext{
		FirstInstall: false,
	}
	err = m.backend.UnlinkSnap(info, linkCtx, NewTaskProgressAdapterLocked(t))
	if err != nil {
		return err
	}

	// add to the disabled services list in snapst services which were disabled
	// when stop-snap-services ran, for usage across changes like in reverting
	// and enabling after being disabled.
	// we keep what's already in the list in snapst because that list is
	// services which were previously present in the snap and disabled, but are
	// no longer present.
	snapst.LastActiveDisabledServices = append(
		snapst.LastActiveDisabledServices,
		snapsup.LastActiveDisabledServices...,
	)

	// Notify link snap participants about link changes.
	notifyLinkParticipants(t, snapsup.InstanceName())

	// mark as inactive
	snapst.Active = false
	Set(st, snapsup.InstanceName(), snapst)

	// Notify link snap participants about link changes.
	notifyLinkParticipants(t, snapsup.InstanceName())

	return err
}

func (m *SnapManager) undoUnlinkSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(t)
	defer perfTimings.Save(st)

	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	isInstalled := snapst.IsInstalled()
	if !isInstalled {
		return fmt.Errorf("internal error: snap %q not installed anymore", snapsup.InstanceName())
	}

	info, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}

	deviceCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return err
	}

	// undo here may be part of failed snap remove change, in which case a later
	// "clear-snap" task could have been executed and some or all of the
	// data of this snap could be lost. If that's the case, then we should not
	// enable the snap back.
	// XXX: should make an exception for snapd/core?
	place := snapsup.placeInfo()
	for _, dir := range []string{place.DataDir(), place.CommonDataDir()} {
		if exists, _, _ := osutil.DirExists(dir); !exists {
			t.Logf("cannot link snap %q back, some of its data has already been removed", snapsup.InstanceName())
			// TODO: mark the snap broken at the SnapState level when we have
			// such concept.
			return nil
		}
	}

	snapst.Active = true
	Set(st, snapsup.InstanceName(), snapst)

	vitalityRank, err := vitalityRank(st, snapsup.InstanceName())
	if err != nil {
		return err
	}
	linkCtx := backend.LinkContext{
		FirstInstall: false,
		VitalityRank: vitalityRank,
	}
	reboot, err := m.backend.LinkSnap(info, deviceCtx, linkCtx, perfTimings)
	if err != nil {
		return err
	}

	// Notify link snap participants about link changes.
	notifyLinkParticipants(t, snapsup.InstanceName())

	// if we just linked back a core snap, request a restart
	// so that we switch executing its snapd.
	m.maybeRestart(t, info, reboot, deviceCtx)

	return nil
}

func (m *SnapManager) doClearSnapData(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	snapsup, snapst, err := snapSetupAndState(t)
	st.Unlock()
	if err != nil {
		return err
	}

	st.Lock()
	info, err := Info(t.State(), snapsup.InstanceName(), snapsup.Revision())
	st.Unlock()
	if err != nil {
		return err
	}

	if err = m.backend.RemoveSnapData(info); err != nil {
		return err
	}

	if len(snapst.Sequence) == 1 {
		// Only remove data common between versions if this is the last version
		if err = m.backend.RemoveSnapCommonData(info); err != nil {
			return err
		}

		st.Lock()
		defer st.Unlock()

		otherInstances, err := hasOtherInstances(st, snapsup.InstanceName())
		if err != nil {
			return err
		}
		// Snap data directory can be removed now too
		if err := m.backend.RemoveSnapDataDir(info, otherInstances); err != nil {
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

	deviceCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return err
	}

	if snapst.Current == snapsup.Revision() && snapst.Active {
		return fmt.Errorf("internal error: cannot discard snap %q: still active", snapsup.InstanceName())
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
	err = m.backend.RemoveSnapFiles(snapsup.placeInfo(), typ, nil, deviceCtx, pb)
	if err != nil {
		t.Errorf("cannot remove snap file %q, will retry in 3 mins: %s", snapsup.InstanceName(), err)
		return &state.Retry{After: 3 * time.Minute}
	}
	if len(snapst.Sequence) == 0 {
		// Remove configuration associated with this snap.
		err = config.DeleteSnapConfig(st, snapsup.InstanceName())
		if err != nil {
			return err
		}
		err = m.backend.DiscardSnapNamespace(snapsup.InstanceName())
		if err != nil {
			t.Errorf("cannot discard snap namespace %q, will retry in 3 mins: %s", snapsup.InstanceName(), err)
			return &state.Retry{After: 3 * time.Minute}
		}
		err = m.backend.RemoveSnapInhibitLock(snapsup.InstanceName())
		if err != nil {
			return err
		}
		if err := m.removeSnapCookie(st, snapsup.InstanceName()); err != nil {
			return fmt.Errorf("cannot remove snap cookie: %v", err)
		}

		otherInstances, err := hasOtherInstances(st, snapsup.InstanceName())
		if err != nil {
			return err
		}

		if err := m.backend.RemoveSnapDir(snapsup.placeInfo(), otherInstances); err != nil {
			return fmt.Errorf("cannot remove snap directory: %v", err)
		}

		// try to remove the auxiliary store info
		if err := discardAuxStoreInfo(snapsup.SideInfo.SnapID); err != nil {
			logger.Noticef("Cannot remove auxiliary store info for %q: %v", snapsup.InstanceName(), err)
		}

		// XXX: also remove sequence files?
	}
	if err = config.DiscardRevisionConfig(st, snapsup.InstanceName(), snapsup.Revision()); err != nil {
		return err
	}
	Set(st, snapsup.InstanceName(), snapst)
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

func (m *SnapManager) doSetAutoAliases(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	snapName := snapsup.InstanceName()
	curInfo, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}

	// --unaliased
	if snapsup.Unaliased {
		t.Set("old-auto-aliases-disabled", snapst.AutoAliasesDisabled)
		snapst.AutoAliasesDisabled = true
	}

	curAliases := snapst.Aliases
	// TODO: implement --prefer logic
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

func (m *SnapManager) doRemoveAliases(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	snapName := snapsup.InstanceName()

	err = m.backend.RemoveSnapAliases(snapName)
	if err != nil {
		return err
	}

	snapst.AliasesPending = true
	Set(st, snapName, snapst)
	return nil
}

func (m *SnapManager) doSetupAliases(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	snapName := snapsup.InstanceName()
	curAliases := snapst.Aliases

	_, _, err = applyAliasesChange(snapName, autoDis, nil, snapst.AutoAliasesDisabled, curAliases, m.backend, doApply)
	if err != nil {
		return err
	}

	snapst.AliasesPending = false
	Set(st, snapName, snapst)
	return nil
}

func (m *SnapManager) doRefreshAliases(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	snapName := snapsup.InstanceName()
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
		if _, _, err := applyAliasesChange(snapName, autoDisabled, curAliases, autoDisabled, newAliases, m.backend, doApply); err != nil {
			return err
		}
	}

	t.Set("old-aliases-v2", curAliases)
	snapst.Aliases = newAliases
	Set(st, snapName, snapst)
	return nil
}

func (m *SnapManager) undoRefreshAliases(t *state.Task, _ *tomb.Tomb) error {
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
	snapName := snapsup.InstanceName()
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
		if _, _, err := applyAliasesChange(snapName, curAutoDisabled, curAliases, autoDisabled, oldAliases, m.backend, doApply); err != nil {
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
			if _, _, err := applyAliasesChange(otherSnap, otherSnapState.AutoAliasesDisabled, otherSnapState.Aliases, newSnapSt.AutoAliasesDisabled, newSnapSt.Aliases, m.backend, doApply); err != nil {
				return err
			}
		}
	}

	for instanceName, snapst := range newSnapStates {
		if conflicting[instanceName] {
			// keep as it was
			continue
		}
		Set(st, instanceName, snapst)
	}
	return nil
}

func (m *SnapManager) doPruneAutoAliases(t *state.Task, _ *tomb.Tomb) error {
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
	snapName := snapsup.InstanceName()
	autoDisabled := snapst.AutoAliasesDisabled
	curAliases := snapst.Aliases

	newAliases := pruneAutoAliases(curAliases, which)

	if !snapst.AliasesPending {
		if _, _, err := applyAliasesChange(snapName, autoDisabled, curAliases, autoDisabled, newAliases, m.backend, doApply); err != nil {
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

func (m *SnapManager) doAlias(t *state.Task, _ *tomb.Tomb) error {
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

	snapName := snapsup.InstanceName()
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

func (m *SnapManager) doDisableAliases(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	snapName := snapsup.InstanceName()

	oldAutoDisabled := snapst.AutoAliasesDisabled
	oldAliases := snapst.Aliases
	newAliases, _ := disableAliases(oldAliases)

	added, removed, err := applyAliasesChange(snapName, oldAutoDisabled, oldAliases, autoDis, newAliases, m.backend, snapst.AliasesPending)
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

func (m *SnapManager) doUnalias(t *state.Task, _ *tomb.Tomb) error {
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
	snapName := snapsup.InstanceName()

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

func (m *SnapManager) doPreferAliases(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	instanceName := snapsup.InstanceName()

	if !snapst.AutoAliasesDisabled {
		// already enabled, nothing to do
		return nil
	}

	curAliases := snapst.Aliases
	aliasConflicts, err := checkAliasesConflicts(st, instanceName, autoEn, curAliases, nil)
	conflErr, isConflErr := err.(*AliasConflictError)
	if err != nil && !isConflErr {
		return err
	}
	if isConflErr && conflErr.Conflicts == nil {
		// it's a snap command namespace conflict, we cannot remedy it
		return conflErr
	}
	// proceed to disable conflicting aliases as needed
	// before re-enabling instanceName aliases

	otherSnapStates := make(map[string]*SnapState, len(aliasConflicts))
	otherSnapDisabled := make(map[string]*otherDisabledAliases, len(aliasConflicts))
	for otherSnap := range aliasConflicts {
		var otherSnapState SnapState
		err := Get(st, otherSnap, &otherSnapState)
		if err != nil {
			return err
		}

		otherAliases, disabledManual := disableAliases(otherSnapState.Aliases)

		added, removed, err := applyAliasesChange(otherSnap, otherSnapState.AutoAliasesDisabled, otherSnapState.Aliases, autoDis, otherAliases, m.backend, otherSnapState.AliasesPending)
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

	added, removed, err := applyAliasesChange(instanceName, autoDis, curAliases, autoEn, curAliases, m.backend, snapst.AliasesPending)
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
	Set(st, instanceName, snapst)
	return nil
}

// changeReadyUpToTask returns whether all other change's tasks are Ready.
func changeReadyUpToTask(task *state.Task) bool {
	me := task.ID()
	change := task.Change()
	for _, task := range change.Tasks() {
		if me == task.ID() {
			// ignore self
			continue
		}
		if !task.Status().Ready() {
			return false
		}
	}
	return true
}

// refreshedSnaps returns the instance names of the snaps successfully refreshed
// in the last batch of refreshes before the given (re-refresh) task.
//
// It does this by advancing through the given task's change's tasks, keeping
// track of the instance names from the first SnapSetup in every lane, stopping
// when finding the given task, and resetting things when finding a different
// re-refresh task (that indicates the end of a batch that isn't the given one).
func refreshedSnaps(reTask *state.Task) []string {
	// NOTE nothing requires reTask to be a check-rerefresh task, nor even to be in
	// a refresh-ish change, but it doesn't make much sense to call this otherwise.
	tid := reTask.ID()
	laneSnaps := map[int]string{}
	// change.Tasks() preserves the order tasks were added, otherwise it all falls apart
	for _, task := range reTask.Change().Tasks() {
		if task.ID() == tid {
			// we've reached ourselves; we don't care about anything beyond this
			break
		}
		if task.Kind() == "check-rerefresh" {
			// we've reached a previous check-rerefresh (but not ourselves).
			// Only snaps in tasks after this point are of interest.
			laneSnaps = map[int]string{}
		}
		lanes := task.Lanes()
		if len(lanes) != 1 {
			// can't happen, really
			continue
		}
		lane := lanes[0]
		if lane == 0 {
			// not really a lane
			continue
		}
		if task.Status() != state.DoneStatus {
			// ignore non-successful lane (1)
			laneSnaps[lane] = ""
			continue
		}
		if _, ok := laneSnaps[lane]; ok {
			// ignore lanes we've already seen (including ones explicitly ignored in (1))
			continue
		}
		var snapsup SnapSetup
		if err := task.Get("snap-setup", &snapsup); err != nil {
			continue
		}
		laneSnaps[lane] = snapsup.InstanceName()
	}

	snapNames := make([]string, 0, len(laneSnaps))
	for _, name := range laneSnaps {
		if name == "" {
			// the lane was unsuccessful
			continue
		}
		snapNames = append(snapNames, name)
	}
	return snapNames
}

// reRefreshSetup holds the necessary details to re-refresh snaps that need it
type reRefreshSetup struct {
	UserID int `json:"user-id,omitempty"`
	*Flags
}

// reRefreshUpdateMany exists just to make testing simpler
var reRefreshUpdateMany = updateManyFiltered

// reRefreshFilter is an updateFilter that returns whether the given update
// needs a re-refresh because of further epoch transitions available.
func reRefreshFilter(update *snap.Info, snapst *SnapState) bool {
	cur, err := snapst.CurrentInfo()
	if err != nil {
		return false
	}
	return !update.Epoch.Equal(&cur.Epoch)
}

var reRefreshRetryTimeout = time.Second / 10

func (m *SnapManager) doCheckReRefresh(t *state.Task, tomb *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	if numHaltTasks := t.NumHaltTasks(); numHaltTasks > 0 {
		logger.Panicf("Re-refresh task has %d tasks waiting for it.", numHaltTasks)
	}

	if !changeReadyUpToTask(t) {
		return &state.Retry{After: reRefreshRetryTimeout, Reason: "pending refreshes"}
	}
	snaps := refreshedSnaps(t)
	if len(snaps) == 0 {
		// nothing to do (maybe everything failed)
		return nil
	}

	var re reRefreshSetup
	if err := t.Get("rerefresh-setup", &re); err != nil {
		return err
	}
	chg := t.Change()
	updated, tasksets, err := reRefreshUpdateMany(tomb.Context(nil), st, snaps, re.UserID, reRefreshFilter, re.Flags, chg.ID())
	if err != nil {
		return err
	}

	if len(updated) == 0 {
		t.Logf("No re-refreshes found.")
	} else {
		t.Logf("Found re-refresh for %s.", strutil.Quoted(updated))

		for _, taskset := range tasksets {
			chg.AddAll(taskset)
		}
		st.EnsureBefore(0)
	}
	t.SetStatus(state.DoneStatus)

	return nil
}

// InjectTasks makes all the halt tasks of the mainTask wait for extraTasks;
// extraTasks join the same lane and change as the mainTask.
func InjectTasks(mainTask *state.Task, extraTasks *state.TaskSet) {
	lanes := mainTask.Lanes()
	if len(lanes) == 1 && lanes[0] == 0 {
		lanes = nil
	}
	for _, l := range lanes {
		extraTasks.JoinLane(l)
	}

	chg := mainTask.Change()
	// Change shouldn't normally be nil, except for cases where
	// this helper is used before tasks are added to a change.
	if chg != nil {
		chg.AddAll(extraTasks)
	}

	// make all halt tasks of the mainTask wait on extraTasks
	ht := mainTask.HaltTasks()
	for _, t := range ht {
		t.WaitAll(extraTasks)
	}

	// make the extra tasks wait for main task
	extraTasks.WaitFor(mainTask)
}

func InjectAutoConnect(mainTask *state.Task, snapsup *SnapSetup) {
	st := mainTask.State()
	autoConnect := st.NewTask("auto-connect", fmt.Sprintf(i18n.G("Automatically connect eligible plugs and slots of snap %q"), snapsup.InstanceName()))
	autoConnect.Set("snap-setup", snapsup)
	InjectTasks(mainTask, state.NewTaskSet(autoConnect))
	mainTask.Logf("added auto-connect task")
}
