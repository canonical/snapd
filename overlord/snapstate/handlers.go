// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timings"
	userclient "github.com/snapcore/snapd/usersession/client"
	"github.com/snapcore/snapd/wrappers"
)

// SnapServiceOptions is a hook set by servicestate.
var SnapServiceOptions = func(st *state.State, snapInfo *snap.Info, grps map[string]*quota.Group) (opts *wrappers.SnapServiceOptions, err error) {
	panic("internal error: snapstate.SnapServiceOptions is unset")
}

var EnsureSnapAbsentFromQuotaGroup = func(st *state.State, snap string) error {
	panic("internal error: snapstate.EnsureSnapAbsentFromQuotaGroup is unset")
}

var SecurityProfilesRemoveLate = func(snapName string, rev snap.Revision, typ snap.Type) error {
	panic("internal error: snapstate.SecurityProfilesRemoveLate is unset")
}

var cgroupMonitorSnapEnded = cgroup.MonitorSnapEnded

// TaskSnapSetup returns the SnapSetup with task params hold by or referred to by the task.
func TaskSnapSetup(t *state.Task) (*SnapSetup, error) {
	var snapsup SnapSetup

	err := t.Get("snap-setup", &snapsup)
	if err != nil && !errors.Is(err, state.ErrNoState) {
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
	if err != nil && !errors.Is(err, state.ErrNoState) {
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

func findLinkSnapTaskForSnap(st *state.State, snapName string) (*state.Task, error) {
	for _, chg := range st.Changes() {
		if chg.IsReady() {
			continue
		}
		for _, tc := range chg.Tasks() {
			if tc.Status().Ready() {
				continue
			}
			if tc.Kind() == "link-snap" {
				snapsup, err := TaskSnapSetup(tc)
				if err != nil {
					return nil, err
				}
				if snapsup.InstanceName() == snapName {
					return tc, nil
				}
			}
		}
	}

	return nil, nil
}

func isInstalled(st *state.State, snapName string) (bool, error) {
	var snapState SnapState
	err := Get(st, snapName, &snapState)
	if err != nil && !errors.Is(err, state.ErrNoState) {
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

	// if a previous version of snapd persisted Prereq only, fill the contentAttrs.
	// There will be no content attrs, so it will not update an outdated default provider
	if len(snapsup.PrereqContentAttrs) == 0 && len(snapsup.Prereq) != 0 {
		snapsup.PrereqContentAttrs = make(map[string][]string, len(snapsup.Prereq))

		for _, prereq := range snapsup.Prereq {
			snapsup.PrereqContentAttrs[prereq] = nil
		}
	}

	if err := m.installPrereqs(t, base, snapsup.PrereqContentAttrs, snapsup.UserID, perfTimings, snapsup.Flags); err != nil {
		return err
	}

	return nil
}

func (m *SnapManager) installOneBaseOrRequired(t *state.Task, snapName string, contentAttrs []string, requireTypeBase bool, channel string, onInFlight error, userID int, flags Flags) (*state.TaskSet, error) {
	st := t.State()

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
		return updatePrereqIfOutdated(t, snapName, contentAttrs, userID, flags)
	}

	deviceCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return nil, err
	}

	// in progress?
	if linkTask, err := findLinkSnapTaskForSnap(st, snapName); err != nil {
		return nil, err
	} else if linkTask != nil {
		// if we are remodeling, then we should return early due to the way that
		// tasks are ordered by the remodeling code. specifically, all snap
		// downloads during a remodel happen prior to snap installation. thus,
		// we cannot wait for snaps to be installed here. see remodelTasks for
		// more information on how the tasks are ordered.
		if deviceCtx.ForRemodeling() {
			return nil, nil
		}
		return nil, onInFlight
	}

	// not installed, nor queued for install -> install it
	ts, err := InstallWithDeviceContext(context.TODO(), st, snapName, &RevisionOptions{Channel: channel}, userID, Flags{RequireTypeBase: requireTypeBase}, nil, deviceCtx, "")

	// something might have triggered an explicit install while
	// the state was unlocked -> deal with that here by simply
	// retrying the operation.
	if conflErr, ok := err.(*ChangeConflictError); ok {
		// conflicted with an install in the same change, just skip
		if conflErr.ChangeID == t.Change().ID() {
			return nil, nil
		}

		return nil, &state.Retry{After: prerequisitesRetryTimeout}
	}
	return ts, err
}

// updates a prerequisite, if it's not providing a content interface that a plug expects it to
func updatePrereqIfOutdated(t *state.Task, snapName string, contentAttrs []string, userID int, flags Flags) (*state.TaskSet, error) {
	if len(contentAttrs) == 0 {
		return nil, nil
	}

	st := t.State()

	// check if the default provider has all expected content tags
	if ok, err := hasAllContentAttrs(st, snapName, contentAttrs); err != nil {
		return nil, err
	} else if ok {
		return nil, nil
	}

	// this is an optimization since the Update would also detect a conflict
	// but only after accessing the store
	if ok, err := shouldSkipToAvoidConflict(t, snapName); err != nil {
		return nil, err
	} else if ok {
		return nil, nil
	}

	deviceCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return nil, err
	}

	// default provider is missing some content tags (likely outdated) so update it
	ts, err := UpdateWithDeviceContext(st, snapName, nil, userID, flags, nil, deviceCtx, "")
	if err != nil {
		if conflErr, ok := err.(*ChangeConflictError); ok {
			// If we aren't seeded, then it's to early to do any updates and we cannot
			// handle this during seeding, so expect the ChangeConflictError in this scenario.
			if conflErr.ChangeKind == "seed" {
				t.Logf("cannot update %q during seeding, will not have required content %q: %s", snapName, strings.Join(contentAttrs, ", "), conflErr)
				return nil, nil
			}

			// there's already an update for the same snap in this change,
			// just skip this one
			if conflErr.ChangeID == t.Change().ID() {
				return nil, nil
			}

			return nil, &state.Retry{After: prerequisitesRetryTimeout}
		}

		// don't propagate error to avoid failing the main install since the
		// content provider is (for now) a soft dependency
		t.Logf("cannot update %q, will not have required content %q: %s", snapName, strings.Join(contentAttrs, ", "), err)
		return nil, nil
	}

	return ts, nil
}

// Checks for conflicting tasks. Returns true if the operation should be skipped. The error
// can be a state.Retry if the operation should be retried later.
func shouldSkipToAvoidConflict(task *state.Task, snapName string) (bool, error) {
	otherTask, err := findLinkSnapTaskForSnap(task.State(), snapName)
	if err != nil {
		return false, err
	}

	if otherTask == nil {
		return false, nil
	}

	// it's in the same change, so the snap is already going to be installed
	if otherTask.Change().ID() == task.Change().ID() {
		return true, nil
	}

	// it's not in the same change, so retry to avoid conflicting changes to the snap
	return true, &state.Retry{
		After:  prerequisitesRetryTimeout,
		Reason: fmt.Sprintf("conflicting changes on snap %q by task %q", snapName, otherTask.Kind()),
	}
}

// Checks if the snap has slots with "content" attributes matching the
// ones that the snap being installed requires
func hasAllContentAttrs(st *state.State, snapName string, requiredContentAttrs []string) (bool, error) {
	providedContentAttrs := make(map[string]bool)
	repo := ifacerepo.Get(st)

	for _, slot := range repo.Slots(snapName) {
		if slot.Interface != "content" {
			continue
		}

		val, ok := slot.Lookup("content")
		if !ok {
			continue
		}

		contentAttr, ok := val.(string)
		if !ok {
			return false, fmt.Errorf("expected 'content' attribute of slot '%s' (snap: '%s') to be string but was %s", slot.Name, snapName, reflect.TypeOf(val))
		}

		providedContentAttrs[contentAttr] = true
	}

	for _, contentAttr := range requiredContentAttrs {
		if _, ok := providedContentAttrs[contentAttr]; !ok {
			return false, nil
		}
	}

	return true, nil
}

func (m *SnapManager) installPrereqs(t *state.Task, base string, prereq map[string][]string, userID int, tm timings.Measurer, flags Flags) error {
	st := t.State()

	// We try to install all wanted snaps. If one snap cannot be installed
	// because of change conflicts or similar we retry. Only if all snaps
	// can be installed together we add the tasks to the change.
	var tss []*state.TaskSet
	for prereqName, contentAttrs := range prereq {
		var onInFlightErr error = nil
		var err error
		var ts *state.TaskSet
		timings.Run(tm, "install-prereq", fmt.Sprintf("install %q", prereqName), func(timings.Measurer) {
			noTypeBaseCheck := false
			ts, err = m.installOneBaseOrRequired(t, prereqName, contentAttrs, noTypeBaseCheck, defaultPrereqSnapsChannel(), onInFlightErr, userID, flags)
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
			tsBase, err = m.installOneBaseOrRequired(t, base, nil, requireTypeBase, defaultBaseSnapsChannel(), onInFlightErr, userID, Flags{})
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
			tsSnapd, err = m.installOneBaseOrRequired(t, "snapd", nil, noTypeBaseCheck, defaultSnapdSnapsChannel(), onInFlightErr, userID, Flags{})
		})
		if err != nil {
			return prereqError("system snap", "snapd", err)
		}
	}

	// If transactional, use a single lane for all tasks, so when
	// one fails the changes for all affected snaps will be
	// undone. Otherwise, have different lanes per snap so
	// failures only affect the culprit snap.
	var joinLane func(ts *state.TaskSet)
	if flags.Transaction == client.TransactionAllSnaps {
		lanes := t.Lanes()
		if len(lanes) != 1 {
			return fmt.Errorf("internal error: more than one lane (%d) on a transactional action", len(lanes))
		}
		transactionLane := lanes[0]
		joinLane = func(ts *state.TaskSet) { ts.JoinLane(transactionLane) }
	} else {
		joinLane = func(ts *state.TaskSet) { ts.JoinLane(st.NewLane()) }
	}

	chg := t.Change()
	// add all required snaps, no ordering, this will be done in the
	// auto-connect task handler
	for _, ts := range tss {
		joinLane(ts)
		chg.AddAll(ts)
	}
	// add the base if needed, prereqs else must wait on this
	if tsBase != nil {
		joinLane(tsBase)
		for _, t := range chg.Tasks() {
			t.WaitAll(tsBase)
		}
		chg.AddAll(tsBase)
	}
	// add snapd if needed, everything must wait on this
	if tsSnapd != nil {
		joinLane(tsSnapd)
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
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	extra := map[string]string{
		"Channel":  snapsup.Channel,
		"Revision": snapsup.SideInfo.Revision.String(),
	}
	if ubuntuCoreTransitionCount > 0 {
		extra["UbuntuCoreTransitionCount"] = strconv.Itoa(ubuntuCoreTransitionCount)
	}

	// TODO: telemetry about errors here

	return nil
}

func installInfoUnlocked(st *state.State, snapsup *SnapSetup, deviceCtx DeviceContext) (store.SnapActionResult, error) {
	st.Lock()
	defer st.Unlock()
	opts := &RevisionOptions{Channel: snapsup.Channel, CohortKey: snapsup.CohortKey, Revision: snapsup.Revision()}
	return installInfo(context.TODO(), st, snapsup.InstanceName(), opts, snapsup.UserID, Flags{}, deviceCtx)
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

	if err := waitForPreDownload(t, snapsup); err != nil {
		return err
	}

	meter := NewTaskProgressAdapterUnlocked(t)
	targetFn := snapsup.MountFile()

	dlOpts := &store.DownloadOptions{
		Scheduled: snapsup.IsAutoRefresh,
		RateLimit: rate,
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

func waitForPreDownload(task *state.Task, snapsup *SnapSetup) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	tasks, err := findTasksMatchingKindAndSnap(st, "pre-download-snap", snapsup.InstanceName(), snapsup.Revision())
	if err != nil {
		return err
	}

	// if there is a pre-download task for the same snap, wait for it to finish
	for _, preTask := range tasks {
		if preTask.Status() != state.DoingStatus {
			continue
		}

		var taskIDs []string
		if err := preTask.Get("waiting-tasks", &taskIDs); err != nil && !errors.Is(err, &state.NoStateError{}) {
			return err
		}

		if !strutil.ListContains(taskIDs, task.ID()) {
			taskIDs = append(taskIDs, task.ID())
			preTask.Set("waiting-tasks", taskIDs)
		}

		logger.Debugf("Download task %s will wait 1min for pre-download task %s", task.ID(), preTask.ID())
		return &state.Retry{After: 2 * time.Minute}

	}

	return nil
}

func (m *SnapManager) doPreDownloadSnap(t *state.Task, tomb *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	snapsup, theStore, user, err := downloadSnapParams(st, t)
	if err != nil {
		return err
	}

	targetFn := snapsup.MountFile()
	dlOpts := &store.DownloadOptions{
		// pre-downloads are only triggered in auto-refreshes
		Scheduled: true,
		RateLimit: autoRefreshRateLimited(st),
	}

	perfTimings := state.TimingsForTask(t)
	st.Unlock()
	timings.Run(perfTimings, "pre-download", fmt.Sprintf("pre-download snap %q", snapsup.SnapName()), func(timings.Measurer) {
		err = theStore.Download(tomb.Context(nil), snapsup.SnapName(), targetFn, snapsup.DownloadInfo, nil, user, dlOpts)
	})
	st.Lock()
	if err != nil {
		return err
	}
	perfTimings.Save(st)

	var waitingTasks []string
	if err := t.Get("waiting-tasks", &waitingTasks); err != nil && !errors.Is(err, &state.NoStateError{}) {
		return err
	}

	// there are download tasks waiting for this one so unblock them and we don't
	// need to spawn a new change
	if len(waitingTasks) > 0 {
		for _, taskID := range waitingTasks {
			st.Task(taskID).At(time.Time{})
		}

		st.EnsureBefore(0)
		return nil
	}

	// remove snap downloads that are no longer needed
	if err := cleanSnapDownloads(st, snapsup.InstanceName()); err != nil {
		return err
	}

	_, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	info, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}

	snapName := snapsup.InstanceName()
	// TODO: in the future, do a hard check before starting an auto-refresh so there's
	// no chance of the snap starting between changes and preventing it from going through
	err = backend.WithSnapLock(info, func() error {
		return refreshAppsCheck(info)
	})
	if err != nil {
		if !errors.Is(err, &BusySnapError{}) {
			return err
		}

		var refreshInfo *userclient.PendingSnapRefreshInfo
		if err := t.Get("refresh-info", &refreshInfo); err != nil {
			return err
		}

		return asyncRefreshOnSnapClose(m.state, snapName, refreshInfo)
	}

	return continueInhibitedAutoRefresh(st, snapName)
}

// asyncRefreshOnSnapClose asynchronously waits for the snap the close, notifies
// the user and then triggers an auto-refresh.
func asyncRefreshOnSnapClose(st *state.State, snapName string, refreshInfo *userclient.PendingSnapRefreshInfo) error {
	// there's already a goroutine waiting for this snap to close so just notify
	if isSnapMonitored(st, snapName) {
		asyncPendingRefreshNotification(context.TODO(), userclient.New(), refreshInfo)
		return nil
	}

	// monitor the snap until it closes. Use buffered channel to prevent the sender
	// from blocking if the receiver stops before reading from it
	done := make(chan string, 1)
	if err := cgroupMonitorSnapEnded(snapName, done); err != nil {
		return fmt.Errorf("cannot monitor for snap closure: %w", err)
	}

	refreshCtx, abort := context.WithCancel(context.Background())
	if ok, err := addMonitoring(st, snapName, abort); err != nil {
		return fmt.Errorf("cannot save monitoring state for %q: %v", snapName, err)
	} else if !ok {
		// refresh candidate missing, no need to monitor
		return nil
	}

	// notify the user about the blocked refresh
	asyncPendingRefreshNotification(context.TODO(), userclient.New(), refreshInfo)

	go continueRefreshOnSnapClose(st, snapName, done, refreshCtx)
	return nil
}

// addMonitoring adds monitoring info to the persisted and in-memory states.
// Returns true if the monitoring state was saved or false if it wasn't because
// the monitoring shouldn't proceed.
func addMonitoring(st *state.State, snapName string, abort context.CancelFunc) (bool, error) {
	var refreshHints map[string]*refreshCandidate
	if err := st.Get("refresh-candidates", &refreshHints); err != nil {
		if errors.Is(err, &state.NoStateError{}) {
			// the candidate may have been reverted from the channel after the
			// auto-refresh, so it's missing here and there's nothing to refresh to
			logger.Noticef("cannot get refresh candidate for %q (possibly reverted): nothing to refresh", snapName)
			return false, nil
		}

		return false, fmt.Errorf("cannot get refresh-candidates: %v", err)
	} else if _, ok := refreshHints[snapName]; !ok {
		// the candidate may have been reverted from the channel after the
		// auto-refresh, so it's missing here and there's nothing to refresh to
		logger.Noticef("cannot get refresh candidate for %q (possibly reverted): nothing to refresh", snapName)
		return false, nil
	}

	abortChans, err := getMonitoringAborts(st)
	if err != nil {
		return false, err
	}
	if abortChans == nil {
		abortChans = make(map[string]context.CancelFunc)
	}

	refreshHints[snapName].Monitored = true
	st.Set("refresh-candidates", refreshHints)

	abortChans[snapName] = abort
	st.Cache("monitored-snaps", abortChans)

	return true, nil
}

// removeMonitoring removes monitoring state related to the specified snap.
func removeMonitoring(st *state.State, snapName string) error {
	var refreshHints map[string]*refreshCandidate
	if err := st.Get("refresh-candidates", &refreshHints); err != nil {
		return fmt.Errorf("cannot get refresh-candidates: %v", err)
	} else if _, ok := refreshHints[snapName]; !ok {
		return fmt.Errorf(`cannot reset the "monitored" field for %q in "refresh-candidates"`, snapName)
	}

	refreshHints[snapName].Monitored = false
	st.Set("refresh-candidates", refreshHints)

	abortChans, err := getMonitoringAborts(st)
	if err != nil {
		return nil
	}
	if abortChans == nil {
		return nil
	}

	delete(abortChans, snapName)
	if len(abortChans) == 0 {
		st.Cache("monitored-snaps", nil)
	} else {
		st.Cache("monitored-snaps", abortChans)
	}

	return nil
}

func continueRefreshOnSnapClose(st *state.State, snapName string, done <-chan string, refreshCtx context.Context) {
	var aborted bool
	select {
	case <-done:
	case <-refreshCtx.Done():
		aborted = true
	}

	st.Lock()
	defer st.Unlock()

	defer func() {
		if err := removeMonitoring(st, snapName); err != nil {
			logger.Noticef("cannot remove monitoring information: %v", err)
		}
	}()

	if aborted {
		logger.Debugf("monitoring for pre-downloaded snap %q was aborted", snapName)
		return
	}

	if err := continueInhibitedAutoRefresh(st, snapName); err != nil {
		logger.Noticef("cannot continue inhibited auto-refresh for %q: %v", snapName, err)
		return
	}
}

// continueInhibitedAutoRefresh refreshes the snap to continue the inhibited auto-refresh
func continueInhibitedAutoRefresh(st *state.State, snapName string) error {
	var refreshHints map[string]*refreshCandidate
	if err := st.Get("refresh-candidates", &refreshHints); err != nil {
		return fmt.Errorf("cannot get refresh-candidates: %v", err)
	}

	hint, ok := refreshHints[snapName]
	if !ok {
		return fmt.Errorf("cannot get refresh-candidates for %q: not found", snapName)
	}

	flags := &Flags{IsAutoRefresh: true, IsContinuedAutoRefresh: true}
	tss, err := autoRefreshPhase2(context.TODO(), st, []*refreshCandidate{hint}, flags, "")
	if err != nil {
		return err
	}

	// TODO: do a check so this can't happen?
	createdPreDl, err := createPreDownloadChange(st, tss)
	if err != nil {
		return err
	}

	if !createdPreDl {
		snaps := []string{snapName}
		msg := autoRefreshSummary(snaps)
		chg := st.NewChange("auto-refresh", msg)
		for _, ts := range tss.Refresh {
			chg.AddAll(ts)
		}
		chg.Set("snap-names", snaps)
		chg.Set("api-data", map[string]interface{}{"snap-names": snaps})
	}

	st.EnsureBefore(0)
	return nil
}

func getMonitoringAborts(st *state.State) (map[string]context.CancelFunc, error) {
	stored := st.Cached("monitored-snaps")
	if stored == nil {
		return nil, nil
	}
	aborts, ok := stored.(map[string]context.CancelFunc)
	if !ok {
		// NOTE: should never happen save for programmer error
		return nil, fmt.Errorf(`internal error: "monitored-snaps" should be map[string]context.CancelFunc but got %T`, stored)
	}
	return aborts, nil
}

func monitoringAbort(st *state.State, snapName string) context.CancelFunc {
	aborts, err := getMonitoringAborts(st)
	if err != nil {
		logger.Noticef("%v", err)
	}
	return aborts[snapName]
}

func isSnapMonitored(st *state.State, snapName string) bool {
	return monitoringAbort(st, snapName) != nil
}

func abortMonitoring(st *state.State, snapName string) {
	if abort := monitoringAbort(st, snapName); abort != nil {
		abort()
	}
}

var (
	mountPollInterval = 1 * time.Second
)

// hasOtherInstances checks whether there are other instances of the snap, be it
// instance keyed or not
func hasOtherInstances(st *state.State, instanceName string) (bool, error) {
	snapName, _ := snap.SplitInstanceName(instanceName)
	var all map[string]*json.RawMessage
	if err := st.Get("snaps", &all); err != nil && !errors.Is(err, state.ErrNoState) {
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

var ErrKernelGadgetUpdateTaskMissing = errors.New("cannot refresh kernel with change created by old snapd that is missing gadget update task")

func checkKernelHasUpdateAssetsTask(t *state.Task) error {
	for _, other := range t.Change().Tasks() {
		snapsup, err := TaskSnapSetup(other)
		if errors.Is(err, state.ErrNoState) {
			// XXX: hooks have no snapsup, is this detection okay?
			continue
		}
		if err != nil {
			return err
		}
		if snapsup.Type != "kernel" {
			continue
		}
		if other.Kind() == "update-gadget-assets" {
			return nil
		}
	}
	return ErrKernelGadgetUpdateTaskMissing
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

	// check that there is a "update-gadget-assets" task for kernels too,
	// see https://bugs.launchpad.net/snapd/+bug/1940553
	if snapsup.Type == snap.TypeKernel {
		st.Lock()
		err = checkKernelHasUpdateAssetsTask(t)
		st.Unlock()
		if err != nil {
			return err
		}
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

	setupOpts := &backend.SetupSnapOptions{
		SkipKernelExtraction: snapsup.SkipKernelExtraction,
	}
	pb := NewTaskProgressAdapterUnlocked(t)
	// TODO Use snapsup.Revision() to obtain the right info to mount
	//      instead of assuming the candidate is the right one.
	var snapType snap.Type
	var installRecord *backend.InstallRecord
	timings.Run(perfTimings, "setup-snap", fmt.Sprintf("setup snap %q", snapsup.InstanceName()), func(timings.Measurer) {
		snapType, installRecord, err = m.backend.SetupSnap(snapsup.SnapPath, snapsup.InstanceName(), snapsup.SideInfo, deviceCtx, setupOpts, pb)
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
			logger.Debugf("snap %q (%v) available at %q", snapsup.InstanceName(), snapsup.Revision(), snapsup.placeInfo().MountDir())
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
	if errors.Is(err, state.ErrNoState) {
		typ = "app"
	} else if err != nil {
		return err
	}

	var installRecord backend.InstallRecord
	st.Lock()
	// install-record is optional (e.g. not present in tasks from older snapd)
	err = t.Get("install-record", &installRecord)
	st.Unlock()
	if err != nil && !errors.Is(err, state.ErrNoState) {
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

type unlinkReason string

const (
	unlinkReasonRefresh       unlinkReason = "refresh"
	unlinkReasonHomeMigration unlinkReason = "home-migration"
)

// restoreUnlinkOnError assumes that state is locked.
func (m *SnapManager) restoreUnlinkOnError(t *state.Task, info *snap.Info, tm timings.Measurer) error {
	st := t.State()

	deviceCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return err
	}

	opts, err := SnapServiceOptions(st, info, nil)
	if err != nil {
		return err
	}
	linkCtx := backend.LinkContext{
		FirstInstall:   false,
		IsUndo:         true,
		ServiceOptions: opts,
	}
	_, err = m.backend.LinkSnap(info, deviceCtx, linkCtx, tm)
	return err
}

func (m *SnapManager) doUnlinkCurrentSnap(t *state.Task, _ *tomb.Tomb) (err error) {
	// called only during refresh when a new revision of a snap is being
	// installed
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

	tr := config.NewTransaction(st)
	experimentalRefreshAppAwareness, err := features.Flag(tr, features.RefreshAppAwareness)
	if err != nil && !config.IsNoOption(err) {
		return err
	}

	refreshAppAwarenessEnabled := experimentalRefreshAppAwareness && !excludeFromRefreshAppAwareness(snapsup.Type)
	if refreshAppAwarenessEnabled && !snapsup.Flags.IgnoreRunning {
		// Invoke the hard refresh flow. Upon success the returned lock will be
		// held to prevent snap-run from advancing until UnlinkSnap, executed
		// below, completes.
		// XXX: should we skip it if type is snap.TypeSnapd?
		lock, err := hardEnsureNothingRunningDuringRefresh(m.backend, st, snapst, snapsup, oldInfo)
		if err != nil {
			var busyErr *timedBusySnapError
			if errors.As(err, &busyErr) {
				// notify user to close the snap and trigger the auto-refresh once it's closed
				refreshInfo := busyErr.PendingSnapRefreshInfo()
				if err := asyncRefreshOnSnapClose(m.state, snapsup.InstanceName(), refreshInfo); err != nil {
					return err
				}
			}

			return err
		}

		defer lock.Close()
	}

	snapst.Active = false

	// snapd current symlink on the refresh path can only replaced by a
	// symlink to a new revision of the snapd snap, so only do the actual
	// unlink if we're not working on the snapd snap
	if oldInfo.Type() != snap.TypeSnapd {
		var reason unlinkReason
		if err := t.Get("unlink-reason", &reason); err != nil && !errors.Is(err, state.ErrNoState) {
			return err
		}
		experimentalRefreshAppAwarenessUX, err := features.Flag(tr, features.RefreshAppAwarenessUX)
		if err != nil && !config.IsNoOption(err) {
			return err
		}
		skipBinaries := reason == unlinkReasonRefresh && refreshAppAwarenessEnabled && experimentalRefreshAppAwarenessUX
		// do the final unlink
		linkCtx := backend.LinkContext{
			FirstInstall: false,
			// This task is only used for unlinking a snap during refreshes so we
			// can safely hard-code this condition here.
			RunInhibitHint: runinhibit.HintInhibitedForRefresh,
			SkipBinaries:   skipBinaries,
		}
		err = m.backend.UnlinkSnap(oldInfo, linkCtx, NewTaskProgressAdapterLocked(t))
		if err != nil {
			if relinkErr := m.restoreUnlinkOnError(t, oldInfo, perfTimings); relinkErr != nil {
				return relinkErr
			}
			return err
		}
	}

	// mark as inactive
	Set(st, snapsup.InstanceName(), snapst)

	// Notify link snap participants about link changes.
	notifyLinkParticipants(t, snapsup)

	// undo migration if appropriate
	if snapsup.Flags.Revert {
		opts, err := getDirMigrationOpts(st, snapst, snapsup)
		if err != nil {
			return err
		}

		newInfo, err := readInfo(snapsup.InstanceName(), snapsup.SideInfo, errorOnBroken)
		if err != nil {
			return err
		}

		action := triggeredMigration(oldInfo.Base, newInfo.Base, opts)
		switch action {
		case full:
			// we're reverting forward to a core22 based revision, so we already
			// migrated previously and should use the ~/Snap sub dir as HOME again
			snapsup.EnableExposedHome = true
			fallthrough
		case hidden:
			if err := m.backend.HideSnapData(snapsup.InstanceName()); err != nil {
				return err
			}

			snapsup.MigratedHidden = true
		case home:
			// we're reverting forward to a core22 based revision, so we already
			// migrated previously and should use the ~/Snap sub dir as HOME again
			snapsup.EnableExposedHome = true

		case revertFull:
			snapsup.DisableExposedHome = true
			fallthrough
		case revertHidden:
			if err := m.backend.UndoHideSnapData(snapsup.InstanceName()); err != nil {
				return err
			}

			snapsup.UndidHiddenMigration = true
		case disableHome:
			snapsup.DisableExposedHome = true
		}

		if err = SetTaskSnapSetup(t, snapsup); err != nil {
			return err
		}
	}

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

	// in a revert, the migration actions were done in doUnlinkCurrentSnap so we
	// revert them here and set SnapSetup flags (which will be used to set the
	// state below)
	if snapsup.Revert {
		if snapsup.EnableExposedHome {
			snapsup.DisableExposedHome = true
			snapsup.EnableExposedHome = false
		} else if snapsup.DisableExposedHome {
			snapsup.DisableExposedHome = false
			snapsup.EnableExposedHome = true
		}

		if snapsup.MigratedHidden {
			if err := m.backend.UndoHideSnapData(snapsup.InstanceName()); err != nil {
				return err
			}

			snapsup.UndidHiddenMigration = true
			snapsup.MigratedHidden = false
		} else if snapsup.UndidHiddenMigration {
			if err := m.backend.HideSnapData(snapsup.InstanceName()); err != nil {
				return err
			}

			snapsup.UndidHiddenMigration = false
			snapsup.MigratedHidden = true
		}
	}

	// undo migration-related state changes (set in doLinkSnap). The respective
	// file migrations are undone either above or in undoCopySnapData. State
	// should only be set in tasks that link the snap for safety and consistency.
	setMigrationFlagsInState(snapst, snapsup)

	if err := writeMigrationStatus(t, snapst, snapsup); err != nil {
		return err
	}

	snapst.Active = true

	opts, err := SnapServiceOptions(st, oldInfo, nil)
	if err != nil {
		return err
	}
	linkCtx := backend.LinkContext{
		FirstInstall:   false,
		IsUndo:         true,
		ServiceOptions: opts,
	}
	reboot, err := m.backend.LinkSnap(oldInfo, deviceCtx, linkCtx, perfTimings)
	if err != nil {
		return err
	}

	// mark as active again
	Set(st, snapsup.InstanceName(), snapst)

	// Notify link snap participants about link changes.
	notifyLinkParticipants(t, snapsup)

	// if we just put back a previous a core snap, request a restart
	// so that we switch executing its snapd
	return m.finishTaskWithMaybeRestart(t, state.UndoneStatus, restartPossibility{info: oldInfo, RebootInfo: reboot})
}

func (m *SnapManager) doCopySnapData(t *state.Task, _ *tomb.Tomb) (err error) {
	st := t.State()
	st.Lock()
	snapsup, snapst, err := snapSetupAndState(t)
	st.Unlock()
	if err != nil {
		return err
	}

	newInfo, err := readInfo(snapsup.InstanceName(), snapsup.SideInfo, errorOnBroken)
	if err != nil {
		return err
	}

	oldInfo, err := snapst.CurrentInfo()
	if err != nil && err != ErrNoCurrent {
		return err
	}

	st.Lock()
	deviceCtx, err := DeviceCtx(st, t, nil)
	st.Unlock()
	if err != nil {
		return err
	}

	st.Lock()
	opts, err := getDirMigrationOpts(st, snapst, snapsup)
	st.Unlock()
	if err != nil {
		return err
	}

	dirOpts := opts.getSnapDirOpts()
	pb := NewTaskProgressAdapterUnlocked(t)
	if copyDataErr := m.backend.CopySnapData(newInfo, oldInfo, dirOpts, pb); copyDataErr != nil {
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

	if err := m.backend.SetupSnapSaveData(newInfo, deviceCtx, pb); err != nil {
		return err
	}

	var oldBase string
	if oldInfo != nil {
		oldBase = oldInfo.Base
	}

	snapName := snapsup.InstanceName()
	switch triggeredMigration(oldBase, newInfo.Base, opts) {
	case hidden:
		if err := m.backend.HideSnapData(snapName); err != nil {
			return err
		}

		snapsup.MigratedHidden = true
	case revertHidden:
		if err := m.backend.UndoHideSnapData(snapName); err != nil {
			return err
		}

		snapsup.UndidHiddenMigration = true
	case full:
		if err := m.backend.HideSnapData(snapName); err != nil {
			return err
		}

		snapsup.MigratedHidden = true
		fallthrough
	case home:
		undo, err := m.backend.InitExposedSnapHome(snapName, newInfo.Revision, opts.getSnapDirOpts())
		if err != nil {
			return err
		}
		st.Lock()
		t.Set("undo-exposed-home-init", undo)
		st.Unlock()

		snapsup.MigratedToExposedHome = true

		// no specific undo action is needed since undoing the copy will undo this
		if err := m.backend.InitXDGDirs(newInfo); err != nil {
			return err
		}
	}

	st.Lock()
	defer st.Unlock()
	return SetTaskSnapSetup(t, snapsup)
}

type migration string

const (
	// none states that no action should be taken
	none migration = "none"
	// hidden migrates ~/snap to ~/.snap
	hidden migration = "hidden"
	// revertHidden undoes the hidden migration (i.e., moves ~/.snap to ~/snap)
	revertHidden migration = "revertHidden"
	// home migrates the new home to ~/Snap
	home migration = "home"
	// full migrates ~/snap to ~/.snap and the new home to ~/Snap
	full migration = "full"
	// disableHome disables ~/Snap as HOME
	disableHome migration = "disableHome"
	// revertFull disables ~/Snap as HOME and undoes the hidden migration
	revertFull migration = "revertFull"
)

func triggeredMigration(oldBase, newBase string, opts *dirMigrationOptions) migration {
	if !opts.MigratedToHidden && opts.UseHidden {
		// flag is set and not migrated yet
		return hidden
	}

	if opts.MigratedToHidden && !opts.UseHidden {
		// migration was done but flag was unset
		return revertHidden
	}

	return none
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

	st.Lock()
	deviceCtx, err := DeviceCtx(st, t, nil)
	st.Unlock()
	if err != nil {
		return err
	}

	// undo migration actions performed in doCopySnapData and set SnapSetup flags
	// accordingly (they're used in undoUnlinkCurrentSnap to set SnapState)
	if snapsup.MigratedToExposedHome || snapsup.MigratedHidden || snapsup.UndidHiddenMigration {
		if snapsup.MigratedToExposedHome {
			var undoInfo backend.UndoInfo

			st.Lock()
			err := t.Get("undo-exposed-home-init", &undoInfo)
			st.Unlock()
			if err != nil {
				return err
			}

			if err := m.backend.UndoInitExposedSnapHome(snapsup.InstanceName(), &undoInfo); err != nil {
				return err
			}

			snapsup.MigratedToExposedHome = false
			snapsup.RemovedExposedHome = true
		}

		if snapsup.MigratedHidden {
			if err := m.backend.UndoHideSnapData(snapsup.InstanceName()); err != nil {
				return err
			}

			snapsup.MigratedHidden = false
			snapsup.UndidHiddenMigration = true
		} else if snapsup.UndidHiddenMigration {
			if err := m.backend.HideSnapData(snapsup.InstanceName()); err != nil {
				return err
			}

			snapsup.MigratedHidden = true
			snapsup.UndidHiddenMigration = false
		}

		st.Lock()
		err = SetTaskSnapSetup(t, snapsup)
		st.Unlock()
		if err != nil {
			return err
		}
	}

	st.Lock()
	opts, err := getDirMigrationOpts(st, snapst, snapsup)
	st.Unlock()
	if err != nil {
		return fmt.Errorf("failed to get snap dir options: %w", err)
	}

	dirOpts := opts.getSnapDirOpts()
	pb := NewTaskProgressAdapterUnlocked(t)
	if err := m.backend.UndoCopySnapData(newInfo, oldInfo, dirOpts, pb); err != nil {
		return err
	}
	if err := m.backend.UndoSetupSnapSaveData(newInfo, oldInfo, deviceCtx, pb); err != nil {
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

// writeMigrationStatus writes the SnapSetup, state and sequence file (if they
// exist). This must be called after the migration undo procedure is done since
// only then do we know the actual final state of the migration. State must be
// locked by caller.
func writeMigrationStatus(t *state.Task, snapst *SnapState, snapsup *SnapSetup) error {
	st := t.State()

	if err := SetTaskSnapSetup(t, snapsup); err != nil {
		return err
	}

	snapName := snapsup.InstanceName()
	err := Get(st, snapName, &SnapState{})
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	if err == nil {
		// migration state might've been written in the change; update it after undo
		Set(st, snapName, snapst)
	}

	seqFile := filepath.Join(dirs.SnapSeqDir, snapName+".json")
	if osutil.FileExists(seqFile) {
		// might've written migration status to seq file in the change; update it
		// after undo
		return writeSeqFile(snapName, snapst)
	}

	// never got to write seq file; don't need to re-write migration status in it
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

	// try to remove trashed any data in ~/snap and ~/.snap/data
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
		Sequence              sequence.SnapSequence `json:"sequence"`
		Current               string                `json:"current"`
		MigratedHidden        bool                  `json:"migrated-hidden"`
		MigratedToExposedHome bool                  `json:"migrated-exposed-home"`
	}{
		Sequence: snapst.Sequence,
		Current:  snapst.Current.String(),
		// if the snap state if empty, we're probably undoing a failed install.
		// Reset the flags to false
		MigratedHidden:        len(snapst.Sequence.Revisions) > 0 && snapst.MigratedHidden,
		MigratedToExposedHome: len(snapst.Sequence.Revisions) > 0 && snapst.MigratedToExposedHome,
	})
	if err != nil {
		return err
	}

	return osutil.AtomicWriteFile(p, b, 0644, 0)
}

// missingDisabledServices returns a list of services that are present in
// this snap info and should be disabled as well as a list of disabled
// services that are currently missing (i.e. they were renamed).
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

	return foundSvcs, missingSvcs, nil
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
	SnapLinkageChanged(st *state.State, snapsup *SnapSetup) error
}

// LinkSnapParticipantFunc is an adapter from function to LinkSnapParticipant.
type LinkSnapParticipantFunc func(st *state.State, snapsup *SnapSetup) error

func (f LinkSnapParticipantFunc) SnapLinkageChanged(st *state.State, snapsup *SnapSetup) error {
	return f(st, snapsup)
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

func notifyLinkParticipants(t *state.Task, snapsup *SnapSetup) {
	st := t.State()
	for _, p := range linkSnapParticipants {
		if err := p.SnapLinkageChanged(st, snapsup); err != nil {
			t.Errorf("%v", err)
		}
	}
}

func determineUnlinkTask(t *state.Task) *state.Task {
	for _, wt := range t.WaitTasks() {
		switch wt.Kind() {
		case "unlink-current-snap", "unlink-snap":
			return wt
		}
		if ut := determineUnlinkTask(wt); ut != nil {
			return ut
		}
	}
	return nil
}

// isSingleRebootBoundary returns true if the link-snap is determined to be the
// do-boundary for a single-reboot set up.
func isSingleRebootBoundary(linkSnap *state.Task) bool {
	// link-snap must have do restart bound
	if !restart.TaskIsRestartBoundary(linkSnap, restart.RestartBoundaryDirectionDo) {
		return false
	}
	// unlink-current-snap must not have undo
	if ut := determineUnlinkTask(linkSnap); ut != nil {
		return !restart.TaskIsRestartBoundary(ut, restart.RestartBoundaryDirectionUndo)
	}
	return false
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

	oldInfo, err := snapst.CurrentInfo()
	if err != nil && err != ErrNoCurrent {
		return err
	}

	// find if the snap is already installed before we modify snapst below
	isInstalled := snapst.IsInstalled()

	cand := sequence.NewRevisionSideState(snapsup.SideInfo, nil)
	m.backend.Candidate(cand.Snap)

	oldCandidateIndex := snapst.LastIndex(cand.Snap.Revision)

	var oldRevsBeforeCand []snap.Revision
	if oldCandidateIndex < 0 {
		snapst.Sequence.Revisions = append(snapst.Sequence.Revisions, cand)
	} else if !snapsup.Revert {
		// save the revs before the candidate, so undoLink can account for discarded revs when putting it back
		for _, si := range snapst.Sequence.Revisions[:oldCandidateIndex] {
			oldRevsBeforeCand = append(oldRevsBeforeCand, si.Snap.Revision)
		}
		// remove the old candidate from the sequence, add it at the end
		copy(snapst.Sequence.Revisions[oldCandidateIndex:len(snapst.Sequence.Revisions)-1], snapst.Sequence.Revisions[oldCandidateIndex+1:])
		snapst.Sequence.Revisions[len(snapst.Sequence.Revisions)-1] = cand
	}

	oldCurrent := snapst.Current
	snapst.Current = cand.Snap.Revision
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
	oldLastRefreshTime := snapst.LastRefreshTime
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

	// don't keep the old state because, if we fail, we may or may not be able to
	// revert the migration. We set the migration status after undoing any
	// migration related ops
	setMigrationFlagsInState(snapst, snapsup)

	newInfo, err := readInfo(snapsup.InstanceName(), cand.Snap, 0)
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

	opts, err := SnapServiceOptions(st, newInfo, nil)
	if err != nil {
		return err
	}
	firstInstall := oldCurrent.Unset()
	linkCtx := backend.LinkContext{
		FirstInstall:   firstInstall,
		ServiceOptions: opts,
	}
	// on UC18+, snap tooling comes from the snapd snap so we need generated
	// mount units to depend on the snapd snap mount units
	if !deviceCtx.Classic() && deviceCtx.Model().Base() != "" {
		linkCtx.RequireMountedSnapdSnap = true
	}

	// write sequence file for failover helpers
	if err := writeSeqFile(snapsup.InstanceName(), snapst); err != nil {
		return err
	}

	defer func() {
		// if link snap fails and this is a first install, then we need to clean up
		// the sequence file
		if IsErrAndNotWait(err) && firstInstall {
			snapst.MigratedHidden = false
			snapst.MigratedToExposedHome = false
			if err := writeSeqFile(snapsup.InstanceName(), snapst); err != nil {
				st.Warnf("cannot update sequence file after failed install of %q: %v", snapsup.InstanceName(), err)
			}
		}
	}()

	if err := m.maybeDiscardNamespacesOnSnapdDowngrade(st, newInfo); err != nil {
		return fmt.Errorf("cannot discard preserved namespaces: %v", err)
	}

	if err := m.maybeRemoveAppArmorProfilesOnSnapdDowngrade(st, newInfo); err != nil {
		return fmt.Errorf("cannot discard apparmor profiles: %v", err)
	}

	rebootInfo, err := m.backend.LinkSnap(newInfo, deviceCtx, linkCtx, perfTimings)
	// defer a cleanup helper which will unlink the snap if anything fails after
	// this point
	defer func() {
		if !IsErrAndNotWait(err) {
			return
		}
		// err is not nil, we need to try and unlink the snap to cleanup after
		// ourselves
		var backendErr error
		if newInfo.Type() == snap.TypeSnapd && !firstInstall {
			// snapd snap is special in the sense that we always
			// need the current symlink, so we restore the link to
			// the old revision
			_, backendErr = m.backend.LinkSnap(oldInfo, deviceCtx, linkCtx, perfTimings)
		} else {
			// snapd during first install and all other snaps
			backendErr = m.backend.UnlinkSnap(newInfo, linkCtx, pb)
		}
		if backendErr != nil {
			t.Errorf("cannot cleanup failed attempt at making snap %q available to the system: %v", snapsup.InstanceName(), backendErr)
		}
		notifyLinkParticipants(t, snapsup)
	}()
	if err != nil {
		return err
	}

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

	if len(snapst.Sequence.Revisions) == 1 {
		if err := m.createSnapCookie(st, snapsup.InstanceName()); err != nil {
			return fmt.Errorf("cannot create snap cookie: %v", err)
		}
	}

	// save for undoLinkSnap
	t.Set("old-trymode", oldTryMode)
	t.Set("old-devmode", oldDevMode)
	t.Set("old-jailmode", oldJailMode)
	t.Set("old-classic", oldClassic)
	t.Set("old-ignore-validation", oldIgnoreValidation)
	t.Set("old-channel", oldChannel)
	t.Set("old-current", oldCurrent)
	t.Set("old-candidate-index", oldCandidateIndex)
	t.Set("old-refresh-inhibited-time", oldRefreshInhibitedTime)
	t.Set("old-cohort-key", oldCohortKey)
	t.Set("old-last-refresh-time", oldLastRefreshTime)
	t.Set("old-revs-before-cand", oldRevsBeforeCand)
	if snapsup.Revert {
		t.Set("old-revert-status", snapst.RevertStatus)
		switch snapsup.RevertStatus {
		case NotBlocked:
			if snapst.RevertStatus == nil {
				snapst.RevertStatus = make(map[int]RevertStatus)
			}
			snapst.RevertStatus[oldCurrent.N] = NotBlocked
		default:
			delete(snapst.RevertStatus, oldCurrent.N)
		}
	} else {
		delete(snapst.RevertStatus, cand.Snap.Revision.N)
	}

	// Record the fact that the snap was refreshed successfully.
	snapst.RefreshInhibitedTime = nil
	if !snapsup.Revert {
		now := timeNow()
		snapst.LastRefreshTime = &now
	}

	if cand.Snap.SnapID != "" {
		// write the auxiliary store info
		aux := &auxStoreInfo{
			Media:   snapsup.Media,
			Website: snapsup.Website,
		}
		if err := keepAuxStoreInfo(cand.Snap.SnapID, aux); err != nil {
			return err
		}
		if len(snapst.Sequence.Revisions) == 1 {
			defer func() {
				if IsErrAndNotWait(err) {
					// the install is getting undone, and there are no more of this snap
					// try to remove the aux info we just created
					discardAuxStoreInfo(cand.Snap.SnapID)
				}
			}()
		}
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

	// abort any snap monitoring that may have started in a pre-download task
	abortMonitoring(st, snapsup.InstanceName())

	// Do at the end so we only preserve the new state if it worked.
	Set(st, snapsup.InstanceName(), snapst)

	// Notify link snap participants about link changes.
	notifyLinkParticipants(t, snapsup)

	// Make sure if state commits and snapst is mutated we won't be rerun
	finalStatus := state.DoneStatus

	// Unfortunately this is needed to make sure we actually request a reboot as a part
	// of link-snap for the gadget (which is the task that has a restart-boundary set).
	// The gadget does not by default set `rebootInfo.RebootRequired` as its difficult for
	// the bootstate to track changes done during update of gadget assets, instead
	// the gadget asset tasks sets a boolean value if any restart is required on the change.
	if snapsup.Type == snap.TypeGadget {
		// Default to true if the gadget link-snap task is a restart-boundary for do-path only.
		// In a single-reboot setup, we may rely on the gadget to perform the reboot. In that specific
		// scenario, the reboot link-snap task will have been marked with a Do restart boundary (and not undo!).
		// If we were to do this in all scenarios (i.e just when it has the do), we would impact things like
		// remodel.
		needsReboot := isSingleRebootBoundary(t)
		if err := t.Change().Get("gadget-restart-required", &needsReboot); err != nil && !errors.Is(err, state.ErrNoState) {
			return err
		}
		rebootInfo.RebootRequired = needsReboot
	}

	// if we just installed a core snap, request a restart
	// so that we switch executing its snapd.
	var canReboot bool
	if rebootInfo.RebootRequired {
		var cannotReboot bool
		// system reboot is required, but can this task request that?
		if err := t.Get("cannot-reboot", &cannotReboot); err != nil && !errors.Is(err, state.ErrNoState) {
			return err
		}
		if !cannotReboot {
			// either the task was created before that variable was
			// introduced or the task can request a reboot
			canReboot = true
		} else {
			t.Logf("reboot postponed to later tasks")
		}
	}
	// XXX: This logic looks a bit confusing, and can be replaced once we decide
	// to get rid of the "cannot-reboot" handling. It's still here for backwards
	// compatibility, with previous snapd versions that were using "cannot-reboot"
	// in state for tasks to support single-reboot with base/kernel.
	if !rebootInfo.RebootRequired || canReboot {
		return m.finishTaskWithMaybeRestart(t, finalStatus, restartPossibility{info: newInfo, RebootInfo: rebootInfo})
	} else {
		t.SetStatus(finalStatus)
		return nil
	}
}

func setMigrationFlagsInState(snapst *SnapState, snapsup *SnapSetup) {
	if snapsup.MigratedHidden {
		snapst.MigratedHidden = true
	} else if snapsup.UndidHiddenMigration {
		snapst.MigratedHidden = false
	}

	if snapsup.MigratedToExposedHome || snapsup.EnableExposedHome {
		snapst.MigratedToExposedHome = true
	} else if snapsup.RemovedExposedHome || snapsup.DisableExposedHome {
		snapst.MigratedToExposedHome = false
	}
}

// restartPossibility carries information to decide whether a restart
// of some form is required. Non-nil pointers to values of it
// can be used in task code to signal that the task should return
// invoking finishTaskWithMaybeRestart.
type restartPossibility struct {
	info *snap.Info
	boot.RebootInfo
}

// finishTaskWithMaybeRestart will set the final status for the task
// and schedule a reboot or restart as needed for the just linked snap
// passed in through the restartPossibility parameter, based on the
// snap type.
func (m *SnapManager) finishTaskWithMaybeRestart(t *state.Task, status state.Status, restartPoss restartPossibility) error {
	// Don't restart when preseeding - we will switch to new snapd on
	// first boot.
	if m.preseed {
		return nil
	}

	st := t.State()

	if restartPoss.RebootRequired {
		return FinishTaskWithRestart(t, status, restart.RestartSystem, &restartPoss.RebootInfo)
	}

	typ := restartPoss.info.Type()

	// If the type of the snap requesting this start is non-trivial that either
	// means we are on Ubuntu Core and the type is a base/kernel/gadget which
	// requires a reboot of the system, or that the type is snapd in which case
	// we just do a restart of snapd itself. In these cases restartReason will
	// be non-empty and thus we will perform a restart.
	// If restartReason is empty, then the snap requesting the restart was not
	// a boot participant and thus we don't need to do any sort of restarts as
	// a result of updating this snap.

	restartReason := daemonRestartReason(st, typ)
	if restartReason == "" {
		// no message -> no restart
		return nil
	}

	t.Logf(restartReason)
	return FinishTaskWithRestart(t, status, restart.RestartDaemon, nil)
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

// maybeDiscardNamespacesOnSnapdDowngrade must be called when we are about to
// activate a different snapd version. It checks whether we are performing a
// downgrade to a snapd version that does not support the
// "x-snapd.origin=rootfs" option (which we use when mounting a snap's "/" as a
// tmpfs) and, if so, discards all preserved snap namespaces; failure to do so
// would cause snap-update-ns to misbehave and destroy our namespace.
// This method assumes that the State is locked.
func (m *SnapManager) maybeDiscardNamespacesOnSnapdDowngrade(st *state.State, snapInfo *snap.Info) error {
	if snapInfo.Type() != snap.TypeSnapd || snapInfo.Version == "" {
		return nil
	}

	// Support for "x-snapd.origin=rootfs" was introduced in snapd 2.57
	if compare, err := strutil.VersionCompare(snapInfo.Version, "2.57"); err == nil && compare < 0 {
		logger.Noticef("Downgrading snapd to version %q, discarding preserved namespaces", snapInfo.Version)
		allSnaps, err := All(st)
		if err != nil {
			return err
		}
		for _, snap := range allSnaps {
			logger.Debugf("Discarding namespace for snap %q", snap.InstanceName())
			if err := m.backend.DiscardSnapNamespace(snap.InstanceName()); err != nil {
				// We don't propagate the error as we don't want to block the
				// downgrade for this. Let's just log it down.
				logger.Noticef("WARNING: discarding namespace of snap %q failed, snap might be unusable until next reboot", snap.InstanceName())
			}
		}
	}
	return nil
}

// maybeRemoveAppArmorProfilesOnSnapdDowngrade must be called when we are about
// to activate a different snapd version. It checks whether we are a snapd with
// a vendored AppArmor and whether we are performing a downgrade to an older
// snapd version and, if so, discards all apparmor profiles; failure to do so
// could cause the older snapd to be unable to load the existing apparmor
// profiles generated by the vendored AppArmor parser.  This method assumes that
// the State is locked.
func (m *SnapManager) maybeRemoveAppArmorProfilesOnSnapdDowngrade(st *state.State, snapInfo *snap.Info) error {
	if snapInfo.Type() != snap.TypeSnapd || snapInfo.Version == "" {
		return nil
	}

	// ignore if not seeded yet or if preseeding
	var seeded bool
	err := st.Get("seeded", &seeded)
	if errors.Is(err, state.ErrNoState) || !seeded || snapdenv.Preseeding() {
		return nil
	}

	if apparmor_sandbox.ProbedLevel() == apparmor_sandbox.Unsupported {
		// no apparmor means no parser features
		return nil
	}

	feats, err := apparmor_sandbox.ParserFeatures()
	if err != nil {
		return err
	}

	// if we do not have a vendored AppArmor parser (ie the parser is not
	// snapd-internal) then it is managed by the host and we don't have to
	// worry about it
	if !strutil.ListContains(feats, "snapd-internal") {
		return nil
	}

	// if we are downgrading snapd with a vendored AppArmor parser then
	// remove any existing AppArmor profiles and the system-key to ensure
	// that there are no profiles on disk when the downgraded snapd starts
	// up (as otherwise they might be incompatible with the AppArmor parser
	// that it is using - either because it also has a vendored AppArmor
	// parser, or because it doesn't in which case it will be using the
	// host's AppArmor parser)
	if compare, err := strutil.VersionCompare(snapInfo.Version, snapdtool.Version); err == nil && compare < 0 {
		logger.Noticef("Downgrading snapd to version %q, discarding all existing snap AppArmor profiles", snapInfo.Version)
		if err = m.backend.RemoveAllSnapAppArmorProfiles(); err != nil {
			// We don't propagate the error as we don't want to block the
			// downgrade for this. Let's just log it down.
			logger.Noticef("WARNING: removing AppArmor profiles failed")
		}
		// also remove system-key to ensure the AppArmor profiles get
		// regenerated when the new snapd starts up
		if err = interfaces.RemoveSystemKey(); err != nil {
			logger.Noticef("WARNING: failed to remove system-key")
		}
	}
	return nil
}

// maybeUndoRemodelBootChanges will check if an undo needs to update the
// bootloader. This can happen if e.g. a new kernel gets installed. This
// will switch the bootloader to the new kernel but if the change is later
// undone we need to switch back to the kernel of the old model.
// It returns a non-nil *restartPossibility if a restart should be considered.
func (m *SnapManager) maybeUndoRemodelBootChanges(t *state.Task) (*restartPossibility, error) {
	// get the new and the old model
	deviceCtx, err := DeviceCtx(t.State(), t, nil)
	if err != nil {
		return nil, err
	}
	// we only have an old model if we are in a remodel situation
	if !deviceCtx.ForRemodeling() {
		return nil, nil
	}
	groundDeviceCtx := deviceCtx.GroundContext()
	oldModel := groundDeviceCtx.Model()
	newModel := deviceCtx.Model()

	// check type of the snap we are undoing, only kernel/base/core are
	// relevant
	snapsup, _, err := snapSetupAndState(t)
	if err != nil {
		return nil, err
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
		return nil, nil
	}
	// we can stop if the kernel/base has not changed
	if snapName == newSnapName {
		return nil, nil
	}
	// we can stop if the snap we are looking at is not a kernel/base
	// of the new model
	if snapsup.InstanceName() != newSnapName {
		return nil, nil
	}
	// get info for *old* kernel/base/core and see if we need to reboot
	// TODO: we may need something like infoForDeviceSnap here
	var snapst SnapState
	if err = Get(t.State(), snapName, &snapst); err != nil {
		return nil, err
	}
	info, err := snapst.CurrentInfo()
	if err != nil && err != ErrNoCurrent {
		return nil, err
	}
	bp := boot.Participant(info, info.Type(), groundDeviceCtx)
	rebootInfo, err := bp.SetNextBoot(boot.NextBootContext{BootWithoutTry: true})
	if err != nil {
		return nil, err
	}

	// we may just have switch back to the old kernel/base/core so
	// we may need to restart
	return &restartPossibility{info: info, RebootInfo: rebootInfo}, nil
}

func (m *SnapManager) undoLinkSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	deviceCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return err
	}

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
	if err != nil && !errors.Is(err, state.ErrNoState) {
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
	if err := t.Get("old-refresh-inhibited-time", &oldRefreshInhibitedTime); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	var oldLastRefreshTime *time.Time
	if err := t.Get("old-last-refresh-time", &oldLastRefreshTime); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	var oldCohortKey string
	if err := t.Get("old-cohort-key", &oldCohortKey); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	var oldRevsBeforeCand []snap.Revision
	if err := t.Get("old-revs-before-cand", &oldRevsBeforeCand); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	if len(snapst.Sequence.Revisions) == 1 {
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
		snapst.Sequence.Revisions = append(snapst.Sequence.Revisions[:currentIndex], snapst.Sequence.Revisions[currentIndex+1:]...)
	} else if !isRevert {
		// account for revisions discarded before the install failed
		discarded := countMissingRevs(oldRevsBeforeCand, snapst.Sequence.Revisions)
		oldCandidateIndex -= discarded

		oldCand := snapst.Sequence.Revisions[currentIndex]
		copy(snapst.Sequence.Revisions[oldCandidateIndex+1:], snapst.Sequence.Revisions[oldCandidateIndex:])
		snapst.Sequence.Revisions[oldCandidateIndex] = oldCand
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
	snapst.LastRefreshTime = oldLastRefreshTime
	snapst.CohortKey = oldCohortKey

	if isRevert {
		var oldRevertStatus map[int]RevertStatus
		err := t.Get("old-revert-status", &oldRevertStatus)
		if err != nil && !errors.Is(err, state.ErrNoState) {
			return err
		}
		// may be nil if not set (e.g. created by old snapd)
		snapst.RevertStatus = oldRevertStatus
	}

	newInfo, err := readInfo(snapsup.InstanceName(), snapsup.SideInfo, 0)
	if err != nil {
		return err
	}

	// we need to undo potential changes to current snap configuration (e.g. if
	// modified by post-refresh/install/configure hooks as part of failed
	// refresh/install) by restoring the configuration of "old current".
	// similarly, we need to re-save the disabled services if there is a
	// revision for us to go back to, see comment below for full explanation
	if len(snapst.Sequence.Revisions) > 0 {
		if err = config.RestoreRevisionConfig(st, snapsup.InstanceName(), oldCurrent); err != nil {
			return err
		}
	} else {
		// in the case of an install we need to clear any config
		err = config.DeleteSnapConfig(st, snapsup.InstanceName())
		if err != nil {
			return err
		}
	}

	pb := NewTaskProgressAdapterLocked(t)
	firstInstall := oldCurrent.Unset()
	linkCtx := backend.LinkContext{
		FirstInstall: firstInstall,
		IsUndo:       true,
	}
	var backendErr error
	if newInfo.Type() == snap.TypeSnapd && !firstInstall {
		// snapst has been updated and now is the old revision, since
		// this is not the first install of snapd, it should exist
		var oldInfo *snap.Info
		oldInfo, err := snapst.CurrentInfo()
		if err != nil {
			return err
		}
		// the snapd snap is special in the sense that we need to make
		// sure that a sensible version is always linked as current,
		// also we never reboot when updating snapd snap
		_, backendErr = m.backend.LinkSnap(oldInfo, deviceCtx, linkCtx, perfTimings)
	} else {
		// snapd during first install and all other snaps
		backendErr = m.backend.UnlinkSnap(newInfo, linkCtx, pb)
	}
	if backendErr != nil {
		return backendErr
	}

	// restartPoss will be set if we should maybe schedule a restart
	var restartPoss *restartPossibility
	restartPoss, err = m.maybeUndoRemodelBootChanges(t)
	if err != nil {
		return err
	}

	// restart only when snapd was installed for the first time and the rest of
	// the cleanup is performed by snapd from core;
	// when reverting a subsequent snapd revision, the restart happens in
	// undoLinkCurrentSnap() instead
	if firstInstall && newInfo.Type() == snap.TypeSnapd {
		restartPoss = &restartPossibility{info: newInfo, RebootInfo: boot.RebootInfo{RebootRequired: false}}
	}

	// write sequence file for failover helpers
	if err := writeSeqFile(snapsup.InstanceName(), snapst); err != nil {
		return err
	}
	// mark as inactive
	Set(st, snapsup.InstanceName(), snapst)

	// Notify link snap participants about link changes.
	notifyLinkParticipants(t, snapsup)

	// Finish task: set status, possibly restart

	// Make sure if state commits and snapst is mutated we won't be rerun
	finalStatus := state.UndoneStatus
	t.SetStatus(finalStatus)

	// should we maybe restart?
	if restartPoss != nil {
		return m.finishTaskWithMaybeRestart(t, finalStatus, *restartPoss)
	}

	// If we are on classic and have no previous version of core
	// we may have restarted from a distro package into the core
	// snap. We need to undo that restart here. Instead of in
	// doUnlinkCurrentSnap() like we usually do when going from
	// core snap -> next core snap
	if release.OnClassic && newInfo.Type() == snap.TypeOS && oldCurrent.Unset() {
		t.Logf("Requested daemon restart (undo classic initial core install)")
		return FinishTaskWithRestart(t, finalStatus, restart.RestartDaemon, nil)
	}

	return nil
}

// countMissingRevs counts how many of the revisions aren't present in the sequence
func countMissingRevs(revisions []snap.Revision, revSideInfos []*sequence.RevisionSideState) int {
	var found int
	for _, rev := range revisions {
		for _, si := range revSideInfos {
			if si.Snap.Revision == rev {
				found++
			}
		}
	}

	return len(revisions) - found
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

// installModeDisabledServices returns what services with
// "install-mode: disabled" should be disabled. Only services
// seen for the first time are considered.
func installModeDisabledServices(st *state.State, snapst *SnapState, currentInfo *snap.Info) (svcsToDisable []string, err error) {
	enabledByHookSvcs := map[string]bool{}
	for _, svcName := range snapst.ServicesEnabledByHooks {
		enabledByHookSvcs[svcName] = true
	}

	// find what services the previous snap had
	prevCurrentSvcs := map[string]bool{}
	if psi := snapst.previousSideInfo(); psi != nil {
		var prevCurrentInfo *snap.Info
		prevCurrentInfo, err = Info(st, snapst.InstanceName(), psi.Revision)
		if err != nil {
			return nil, err
		}
		if prevCurrentInfo != nil {
			for _, prevSvc := range prevCurrentInfo.Services() {
				prevCurrentSvcs[prevSvc.Name] = true
			}
		}
	}
	// and deal with "install-mode: disable" for all new services
	// (i.e. not present in previous snap).
	//
	// Services that are not new but have "install-mode: disable"
	// do not need special handling. They are either still disabled
	// or something has enabled them and then they should stay enabled.
	for _, svc := range currentInfo.Services() {
		if svc.InstallMode == "disable" && !enabledByHookSvcs[svc.Name] {
			if !prevCurrentSvcs[svc.Name] {
				svcsToDisable = append(svcsToDisable, svc.Name)
			}
		}
	}
	return svcsToDisable, nil
}

func (m *SnapManager) startSnapServices(t *state.Task, _ *tomb.Tomb) error {
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

	// check if any previously disabled services are now no longer services and
	// log messages about that
	for _, svc := range snapst.LastActiveDisabledServices {
		app, ok := currentInfo.Apps[svc]
		if !ok {
			logger.Noticef("previously disabled service %s no longer exists", svc)
		} else if !app.IsService() {
			logger.Noticef("previously disabled service %s is now an app and not a service", svc)
		}
	}

	// get the services which should be disabled (not started),
	// as well as the services which are not present in this revision, but were
	// present and disabled in a previous one and as such should be kept inside
	// snapst for persistent storage
	svcsToDisable, svcsToSave, err := missingDisabledServices(snapst.LastActiveDisabledServices, currentInfo)
	if err != nil {
		return err
	}

	// check what services with "InstallMode: disable" need to be disabled
	svcsToDisableFromInstallMode, err := installModeDisabledServices(st, snapst, currentInfo)
	if err != nil {
		return err
	}
	svcsToDisable = append(svcsToDisable, svcsToDisableFromInstallMode...)

	// append services that were disabled by hooks (they should not get re-enabled)
	svcsToDisable = append(svcsToDisable, snapst.ServicesDisabledByHooks...)

	// save the current last-active-disabled-services before we re-write it in case we
	// need to undo this
	t.Set("old-last-active-disabled-services", snapst.LastActiveDisabledServices)

	// commit the missing services to state so when we unlink this revision and
	// go to a different revision with potentially different service names, the
	// currently missing service names will be re-disabled if they exist later
	snapst.LastActiveDisabledServices = svcsToSave

	// reset services tracked by operations from hooks
	snapst.ServicesDisabledByHooks = nil
	snapst.ServicesEnabledByHooks = nil
	Set(st, snapsup.InstanceName(), snapst)

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
	err = m.backend.StartServices(startupOrdered, svcsToDisable, pb, perfTimings)
	st.Lock()

	return err
}

func (m *SnapManager) undoStartSnapServices(t *state.Task, _ *tomb.Tomb) error {
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

	var oldLastActiveDisabledServices []string
	if err := t.Get("old-last-active-disabled-services", &oldLastActiveDisabledServices); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	snapst.LastActiveDisabledServices = oldLastActiveDisabledServices
	Set(st, snapsup.InstanceName(), snapst)

	svcs := currentInfo.Services()
	if len(svcs) == 0 {
		return nil
	}

	// XXX: stop reason not set on start task, should we have a new reason for undo?
	var stopReason snap.ServiceStopReason

	// stop the services
	st.Unlock()
	err = m.backend.StopServices(svcs, stopReason, progress.Null, perfTimings)
	st.Lock()
	if err != nil {
		return err
	}

	return nil
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
	if err := t.Get("stop-reason", &stopReason); err != nil && !errors.Is(err, state.ErrNoState) {
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
	// this list is not meant to save what services are disabled at any given
	// time, specifically just what services are disabled while systemd loses
	// track of the services. this list is also used to determine what services are enabled
	// when we start services of a new revision of the snap in
	// start-snap-services handler.
	disabledServices, err := m.queryDisabledServices(currentInfo, pb)
	if err != nil {
		return err
	}

	st.Lock()
	defer st.Unlock()

	// for undo
	t.Set("old-last-active-disabled-services", snapst.LastActiveDisabledServices)
	// undo could queryDisabledServices, but this avoids it
	t.Set("disabled-services", disabledServices)

	// add to the disabled services list in snapst services which were disabled
	// for usage across changes like in reverting and enabling after being
	// disabled.
	// we keep what's already in the list in snapst because that list is
	// services which were previously present in the snap and disabled, but are
	// no longer present.
	snapst.LastActiveDisabledServices = append(
		snapst.LastActiveDisabledServices,
		disabledServices...,
	)

	// reset services tracked by operations from hooks
	snapst.ServicesDisabledByHooks = nil
	snapst.ServicesEnabledByHooks = nil

	Set(st, snapsup.InstanceName(), snapst)

	return nil
}

func (m *SnapManager) undoStopSnapServices(t *state.Task, _ *tomb.Tomb) error {
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

	startupOrdered, err := snap.SortServices(svcs)
	if err != nil {
		return err
	}

	var lastActiveDisabled []string
	if err := t.Get("old-last-active-disabled-services", &lastActiveDisabled); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	snapst.LastActiveDisabledServices = lastActiveDisabled
	Set(st, snapsup.InstanceName(), snapst)

	var disabledServices []string
	if err := t.Get("disabled-services", &disabledServices); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	st.Unlock()
	err = m.backend.StartServices(startupOrdered, disabledServices, progress.Null, perfTimings)
	st.Lock()
	if err != nil {
		return err
	}

	return nil
}

func (m *SnapManager) doUnlinkSnap(t *state.Task, _ *tomb.Tomb) error {
	// invoked only if snap has a current active revision, during remove or
	// disable
	// in case of the snapd snap, we only reach here if disabling or removal
	// was deemed ok by earlier checks

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
	unlinkCtx := backend.LinkContext{
		FirstInstall: false,
	}
	err = m.backend.UnlinkSnap(info, unlinkCtx, NewTaskProgressAdapterLocked(t))
	if err != nil {
		return err
	}

	// mark as inactive
	snapst.Active = false
	Set(st, snapsup.InstanceName(), snapst)

	// Notify link snap participants about link changes.
	notifyLinkParticipants(t, snapsup)

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

	opts, err := SnapServiceOptions(st, info, nil)
	if err != nil {
		return err
	}
	linkCtx := backend.LinkContext{
		FirstInstall:   false,
		IsUndo:         true,
		ServiceOptions: opts,
	}
	reboot, err := m.backend.LinkSnap(info, deviceCtx, linkCtx, perfTimings)
	if err != nil {
		return err
	}

	// Notify link snap participants about link changes.
	notifyLinkParticipants(t, snapsup)

	// if we just linked back a core snap, request a restart
	// so that we switch executing its snapd.
	return m.finishTaskWithMaybeRestart(t, state.UndoneStatus, restartPossibility{info: info, RebootInfo: reboot})
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

	st.Lock()
	opts, err := getDirMigrationOpts(st, snapst, snapsup)
	st.Unlock()
	if err != nil {
		return err
	}

	dirOpts := opts.getSnapDirOpts()
	if err = m.backend.RemoveSnapData(info, dirOpts); err != nil {
		return err
	}

	if len(snapst.Sequence.Revisions) == 1 {
		// Only remove data common between versions if this is the last version
		if err = m.backend.RemoveSnapCommonData(info, dirOpts); err != nil {
			return err
		}

		// Same for the common snap save data directory, we only remove it if this
		// is the last version.
		st.Lock()
		deviceCtx, err := DeviceCtx(t.State(), t, nil)
		st.Unlock()
		if err != nil {
			return err
		}
		if err = m.backend.RemoveSnapSaveData(info, deviceCtx); err != nil {
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

	// drop any potential revert status for this revision
	delete(snapst.RevertStatus, snapsup.Revision().N)

	if len(snapst.Sequence.Revisions) == 1 {
		snapst.Sequence.Revisions = nil
		snapst.Current = snap.Revision{}
	} else {
		newSeq := make([]*sequence.RevisionSideState, 0, len(snapst.Sequence.Revisions))
		for _, si := range snapst.Sequence.Revisions {
			if si.Snap.Revision == snapsup.Revision() {
				// leave out
				continue
			}
			newSeq = append(newSeq, si)
		}
		snapst.Sequence.Revisions = newSeq
		if snapst.Current == snapsup.Revision() {
			snapst.Current = newSeq[len(newSeq)-1].Snap.Revision
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
	if len(snapst.Sequence.Revisions) == 0 {
		if err = m.backend.RemoveContainerMountUnits(snapsup.containerInfo(), nil); err != nil {
			return err
		}

		if err := pruneRefreshCandidates(st, snapsup.InstanceName()); err != nil {
			return err
		}
		if err := pruneSnapsHold(st, snapsup.InstanceName()); err != nil {
			return err
		}

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

		// remove the snap from any quota groups it may have been in, otherwise
		// that quota group may get into an inconsistent state
		if err := EnsureSnapAbsentFromQuotaGroup(st, snapsup.InstanceName()); err != nil {
			return err
		}
	}
	if err = config.DiscardRevisionConfig(st, snapsup.InstanceName(), snapsup.Revision()); err != nil {
		return err
	}
	if err = SecurityProfilesRemoveLate(snapsup.InstanceName(), snapsup.Revision(), snapsup.Type); err != nil {
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

	// --unaliased/--prefer
	// auto aliased is disabled for --prefer to avoid conflicting with
	// existing installs
	if snapsup.Unaliased || snapsup.Prefer {
		t.Set("old-auto-aliases-disabled", snapst.AutoAliasesDisabled)
		snapst.AutoAliasesDisabled = true
	}

	curAliases := snapst.Aliases
	newAliases, err := refreshAliases(st, curInfo, curAliases)
	if err != nil {
		return err
	}
	_, err = checkAliasesConflicts(st, snapName, snapst.AutoAliasesDisabled, newAliases, nil)
	if err != nil {
		return err
	}

	if !snapst.AliasesPending {
		// set-auto-aliases can be invoked in both install and update, and
		// at least in the update case, remove-aliases may not run based
		// on a check, and as such old aliases need to be pruned later
		// in setup-aliases.
		//
		// set this flag to let setup-aliases know that it should
		// prune old aliases.
		//
		// NOTE: this will also trigger for first installs since
		// AliasesPending will also be false for a new install but
		// it is fine since prunning without without existing aliases
		// is basically a no-op.
		t.Set("prune-old-aliases", true)
	}

	t.Set("old-aliases-v2", curAliases)
	snapst.AliasesPending = true
	snapst.Aliases = newAliases
	Set(st, snapName, snapst)
	return nil
}

type removeAliasesReason string

const (
	removeAliasesReasonRefresh removeAliasesReason = "refresh"
	removeAliasesReasonDisable removeAliasesReason = "disable"
	removeAliasesReasonRemove  removeAliasesReason = "remove"
)

// shouldSkipRemoveAliases checks if we should skip removal of aliases for
// experimental RAA UX features, where the app is perceived to be present
// during a refresh.
func shouldSkipRemoveAliases(st *state.State, removeReason removeAliasesReason, snapType snap.Type) (skip bool, err error) {
	tr := config.NewTransaction(st)
	experimentalRefreshAppAwareness, err := features.Flag(tr, features.RefreshAppAwareness)
	if err != nil && !config.IsNoOption(err) {
		return false, err
	}
	experimentalRefreshAppAwarenessUX, err := features.Flag(tr, features.RefreshAppAwarenessUX)
	if err != nil && !config.IsNoOption(err) {
		return false, err
	}

	if removeReason != removeAliasesReasonRefresh {
		return false, nil
	}
	if !experimentalRefreshAppAwarenessUX {
		return false, nil
	}
	if !experimentalRefreshAppAwareness {
		return false, nil
	}
	if excludeFromRefreshAppAwareness(snapType) {
		return false, nil
	}

	return true, nil
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

	var removeReason removeAliasesReason
	if err := t.Get("remove-reason", &removeReason); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	skip, err := shouldSkipRemoveAliases(st, removeReason, snapsup.Type)
	if err != nil {
		return err
	}
	if skip {
		// skip removing aliases, setup-aliases will prune old aliases later.
		return nil
	}

	err = m.backend.RemoveSnapAliases(snapName)
	if err != nil {
		return err
	}

	snapst.AliasesPending = true
	Set(st, snapName, snapst)
	return nil
}

func (m *SnapManager) undoRemoveAliases(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	if !snapst.AliasesPending {
		// do nothing
		return nil
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

func shouldPruneOldAliases(t *state.Task, snapst *SnapState) (prune bool, oldAliases map[string]*AliasTarget, oldAutoDisabled bool, err error) {
	for _, t := range t.WaitTasks() {
		if t.Kind() != "set-auto-aliases" || !t.Status().Ready() {
			continue
		}

		taskSetup, err := TaskSnapSetup(t)
		if err != nil {
			return false, nil, false, err
		}

		if taskSetup.InstanceName() != snapst.InstanceName() {
			continue
		}

		var pruneOldAliases bool
		if err := t.Get("prune-old-aliases", &pruneOldAliases); err != nil && !errors.Is(err, state.ErrNoState) {
			return false, nil, false, err
		}

		if !pruneOldAliases {
			// don't prune
			return false, nil, autoDis, nil
		}

		var oldAliases map[string]*AliasTarget
		if err := t.Get("old-aliases-v2", &oldAliases); err != nil && !errors.Is(err, state.ErrNoState) {
			return false, nil, false, err
		}

		oldAutoDisabled := snapst.AutoAliasesDisabled
		if err := t.Get("old-auto-aliases-disabled", &oldAutoDisabled); err != nil && !errors.Is(err, state.ErrNoState) {
			return false, nil, false, err
		}

		return true, oldAliases, oldAutoDisabled, nil
	}

	return false, nil, autoDis, nil
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
	autoDisabled := snapst.AutoAliasesDisabled

	prune, oldAliases, oldAutoDisabled, err := shouldPruneOldAliases(t, snapst)
	if err != nil {
		return err
	}

	// no need to check for conflicts as it was already checked in `set-auto-aliases`
	_, _, err = applyAliasesChange(snapName, oldAutoDisabled, oldAliases, autoDisabled, curAliases, m.backend, doApply)
	if err != nil {
		// the undo for set-auto-aliases must revert aliases on disk since
		// applyAliasesChange could have failed mid-way leaving disk in an
		// inconsistent state.
		return err
	}

	t.Set("old-aliases-pruned", prune)

	snapst.AliasesPending = false
	Set(st, snapName, snapst)
	return nil
}

func (m *SnapManager) undoSetupAliases(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	snapName := snapsup.InstanceName()

	var oldAliasesPruned bool
	if err := t.Get("old-aliases-pruned", &oldAliasesPruned); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if oldAliasesPruned {
		// keep AliasesPending set to false so that the undo for set-auto-aliases
		// can revert aliases on disk.
		return nil
	}

	// remove added aliases
	err = m.backend.RemoveSnapAliases(snapName)
	if err != nil {
		return err
	}
	snapst.AliasesPending = true
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
	if errors.Is(err, state.ErrNoState) {
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
	if err = t.Get("old-auto-aliases-disabled", &autoDisabled); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	var otherSnapDisabled map[string]*otherDisabledAliases
	if err = t.Get("other-disabled-aliases", &otherSnapDisabled); err != nil && !errors.Is(err, state.ErrNoState) {
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

	var setAutoAliasesInPruneMode bool
	if t.Kind() == "set-auto-aliases" {
		if err := t.Get("prune-old-aliases", &setAutoAliasesInPruneMode); err != nil && !errors.Is(err, state.ErrNoState) {
			return err
		}
	}

	// AliasesPending needs to be fixed if `setup-aliases` failed in prune mode.
	//
	// the following sequence triggers the edge-case:
	//	1. `remove-aliases` skips removing aliases for refresh-app-awareness
	//	    keeping AliasesPending as false
	//	2. `set-auto-aliases` sets AliasesPending to true
	//	3. `setup-aliases` fails mid-way before setting AliasesPending to false
	if snapst.AliasesPending && setAutoAliasesInPruneMode {
		// aliases on disk and state should now be fixed with applyAliasesChange below.
		snapst.AliasesPending = false
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
	if err != nil && !errors.Is(err, state.ErrNoState) {
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
// in the last batch of refreshes before the given (re-refresh) task; failed is
// true if any of the snaps failed to refresh.
//
// It does this by advancing through the given task's change's tasks, and keeping
// track of the instance names from every SnapSetup in "download-snap" tasks it finds.
// It stops when finding the given task, and resetting things when finding a different
// re-refresh task (that indicates the end of a batch that isn't the given one).
func refreshedSnaps(reTask *state.Task) (snapNames []string, failed bool, err error) {
	// NOTE nothing requires reTask to be a check-rerefresh task, nor even to be in
	// a refresh-ish change, but it doesn't make much sense to call this otherwise.
	tid := reTask.ID()
	laneSnaps := make(map[int]map[string]bool)
	failedLanes := make(map[int]bool)
	// change.Tasks() preserves the order tasks were added, otherwise it all falls apart
	for _, task := range reTask.Change().Tasks() {
		if task.ID() == tid {
			// we've reached ourselves; we don't care about anything beyond this
			break
		}
		if task.Kind() == "check-rerefresh" {
			// we've reached a previous check-rerefresh (but not ourselves).
			// Only snaps in tasks after this point are of interest.
			laneSnaps = make(map[int]map[string]bool)
		}

		// Ignore tasks on '0' lane, they are not refreshes anyway.
		taskLanes := task.Lanes()
		if len(taskLanes) == 1 && taskLanes[0] == 0 {
			continue
		}

		// Track lanes that failed.
		if task.Status() != state.DoneStatus {
			for _, l := range taskLanes {
				failedLanes[l] = true
			}
		}

		// Only check "download-snap" as that is the only task we expect to have
		// "snap-setup" attached to it in a refresh context. No point in checking
		// every task in the task-sets.
		var snapsup SnapSetup
		switch task.Kind() {
		case "download-snap":
			if err := task.Get("snap-setup", &snapsup); err != nil {
				return nil, false, fmt.Errorf("internal error: expected SnapSetup for %s: %v", task.Kind(), err)
			}
		default:
			continue
		}

		for _, l := range taskLanes {
			// Add the snap to list of snaps for this lane.
			if snaps := laneSnaps[l]; snaps == nil {
				laneSnaps[l] = make(map[string]bool)
			}
			laneSnaps[l][snapsup.InstanceName()] = true
		}
	}

	snapNames = make([]string, 0, len(laneSnaps))
	for lane, snaps := range laneSnaps {
		// Is it one of the failed lanes?
		if failedLanes[lane] {
			failed = true
			continue
		}
		for name := range snaps {
			snapNames = append(snapNames, name)
		}
	}
	return snapNames, failed, nil
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

var reRefreshRetryTimeout = time.Second / 2

func (m *SnapManager) doCheckReRefresh(t *state.Task, tomb *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	if numHaltTasks := t.NumHaltTasks(); numHaltTasks > 0 {
		logger.Panicf("Re-refresh task has %d tasks waiting for it.", numHaltTasks)
	}

	// Is there a restart pending for the current change? Then wait for
	// restart to happen before proceeding, otherwise we will be blocking
	// any restart that is waiting to occur. We handle this here as this
	// task is dynamically added.
	if restart.PendingForChange(st, t.Change()) {
		return restart.TaskWaitForRestart(t)
	}

	if !changeReadyUpToTask(t) {
		return &state.Retry{After: reRefreshRetryTimeout, Reason: "pending refreshes"}
	}

	snaps, failed, err := refreshedSnaps(t)
	if err != nil {
		return err
	}
	if len(snaps) > 0 {
		if err := pruneRefreshCandidates(st, snaps...); err != nil {
			return err
		}
	}

	chg := t.Change()

	// if any snap failed to refresh, reconsider validation set tracking
	if failed {
		tasksets, err := maybeRestoreValidationSetsAndRevertSnaps(st, snaps, chg.ID())
		if err != nil {
			return err
		}
		if len(tasksets) > 0 {
			chg := t.Change()
			for _, taskset := range tasksets {
				chg.AddAll(taskset)
			}
			st.EnsureBefore(0)
			t.SetStatus(state.DoneStatus)
			return nil
		}
		// else - validation sets tracking got restored or wasn't affected, carry on
	}

	if len(snaps) == 0 {
		// nothing to do (maybe everything failed)
		return nil
	}

	// update validation sets stack: there are two possibilities
	// - if maybeRestoreValidationSetsAndRevertSnaps restored previous tracking
	// or refresh succeeded and it hasn't changed then this is a noop
	// (AddCurrentTrackingToValidationSetsStack ignores tracking if identical
	// to the topmost stack entry);
	// - if maybeRestoreValidationSetsAndRevertSnaps kept new tracking
	// because its constraints were met even after partial failure or
	// refresh succeeded and tracking got updated, then
	// this creates a new copy of validation-sets tracking data.
	if AddCurrentTrackingToValidationSetsStack != nil {
		if err := AddCurrentTrackingToValidationSetsStack(st); err != nil {
			return err
		}
	}

	var re reRefreshSetup
	if err := t.Get("rerefresh-setup", &re); err != nil {
		return err
	}

	updated, updateTss, err := reRefreshUpdateMany(tomb.Context(nil), st, snaps, nil, re.UserID, reRefreshFilter, re.Flags, chg.ID())
	if err != nil {
		return err
	}

	var newTasks bool
	if len(updated) == 0 {
		t.Logf("No re-refreshes found.")
	} else {
		t.Logf("Found re-refresh for %s.", strutil.Quoted(updated))

		for _, taskset := range updateTss.Refresh {
			chg.AddAll(taskset)
			newTasks = true
		}
	}

	if created, err := createPreDownloadChange(st, updateTss); err != nil {
		return err
	} else if created {
		newTasks = true
	}

	if newTasks {
		st.EnsureBefore(0)
	}
	t.SetStatus(state.DoneStatus)

	return nil
}

func (m *SnapManager) doConditionalAutoRefresh(t *state.Task, tomb *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	snaps, err := snapsToRefresh(t)
	if err != nil {
		return err
	}

	if len(snaps) == 0 {
		logger.Debugf("refresh gating: no snaps to refresh")
		return nil
	}

	updateTss, err := autoRefreshPhase2(context.TODO(), st, snaps, nil, t.Change().ID())
	if err != nil {
		return err
	}

	if _, err := createPreDownloadChange(st, updateTss); err != nil {
		return err
	}

	if updateTss.Refresh != nil {
		// update the map of refreshed snaps on the task, this affects
		// conflict checks (we don't want to conflict on snaps that were held and
		// won't be refreshed) -  see conditionalAutoRefreshAffectedSnaps().
		newToUpdate := make(map[string]*refreshCandidate, len(snaps))
		for _, candidate := range snaps {
			newToUpdate[candidate.InstanceName()] = candidate
		}
		t.Set("snaps", newToUpdate)

		// update original auto-refresh change
		chg := t.Change()
		for _, ts := range updateTss.Refresh {
			ts.WaitFor(t)
			chg.AddAll(ts)
		}
	}

	t.SetStatus(state.DoneStatus)
	st.EnsureBefore(0)
	return nil
}

func (m *SnapManager) doMigrateSnapHome(t *state.Task, tomb *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	snapsup, snapst, err := snapSetupAndState(t)
	st.Unlock()
	if err != nil {
		return err
	}

	st.Lock()
	opts, err := getDirMigrationOpts(st, snapst, snapsup)
	st.Unlock()
	if err != nil {
		return err
	}

	dirOpts := opts.getSnapDirOpts()
	undo, err := m.backend.InitExposedSnapHome(snapsup.InstanceName(), snapsup.Revision(), dirOpts)
	if err != nil {
		return err
	}

	st.Lock()
	defer st.Unlock()
	t.Set("undo-exposed-home-init", undo)
	snapsup.MigratedToExposedHome = true

	return SetTaskSnapSetup(t, snapsup)
}

func (m *SnapManager) undoMigrateSnapHome(t *state.Task, tomb *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	snapsup, snapst, err := snapSetupAndState(t)
	st.Unlock()
	if err != nil {
		return err
	}

	var undo backend.UndoInfo

	st.Lock()
	err = t.Get("undo-exposed-home-init", &undo)
	st.Unlock()
	if err != nil {
		return err
	}

	if err := m.backend.UndoInitExposedSnapHome(snapsup.InstanceName(), &undo); err != nil {
		return err
	}

	snapsup.MigratedToExposedHome = false
	snapst.MigratedToExposedHome = false

	st.Lock()
	defer st.Unlock()
	return writeMigrationStatus(t, snapst, snapsup)
}

func (m *SnapManager) decodeValidationSets(t *state.Task) (map[string]*asserts.ValidationSet, error) {
	encodedAsserts := make(map[string][]byte)
	if err := t.Get("validation-sets", &encodedAsserts); err != nil {
		return nil, err
	}

	decodedAsserts := make(map[string]*asserts.ValidationSet, len(encodedAsserts))
	for vsStr, encAssert := range encodedAsserts {
		decAssert, err := asserts.Decode(encAssert)
		if err != nil {
			return nil, err
		}

		vsAssert, ok := decAssert.(*asserts.ValidationSet)
		if !ok {
			return nil, errors.New("expected encoded assertion to be of type ValidationSet")
		}
		decodedAsserts[vsStr] = vsAssert
	}
	return decodedAsserts, nil
}

func (m *SnapManager) doEnforceValidationSets(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	pinnedSeqs := make(map[string]int)
	if err := t.Get("pinned-sequence-numbers", &pinnedSeqs); err != nil {
		return err
	}

	snaps, ignoreValidation, err := InstalledSnaps(st)
	if err != nil {
		return err
	}

	// 'validation-set-keys' determines which enforcement function to invoke. If provided
	// then we should call EnforceLocalValidationSets, which does not
	// fetch any assertions or their pre-requisites. If not provided, then 'validation-sets'
	// must be set, and we call EnforceValidationSets, which may contact the
	// store for any additional assertions.
	var local bool
	vsKeys := make(map[string][]string)
	if err := t.Get("validation-set-keys", &vsKeys); err != nil && !errors.Is(err, &state.NoStateError{}) {
		// we accept NoStateError, it simply means we use the normal version, however then
		// 'userID' and 'validation-sets' must be present
		return err
	} else if err == nil {
		// 'validation-set-keys' was present, use the local version
		local = true
	}

	if local {
		if err := EnforceLocalValidationSets(st, vsKeys, pinnedSeqs, snaps, ignoreValidation); err != nil {
			return fmt.Errorf("cannot enforce validation sets: %v", err)
		}
	} else {
		var userID int
		if err := t.Get("userID", &userID); err != nil {
			return err
		}

		decodedAsserts, err := m.decodeValidationSets(t)
		if err != nil {
			return err
		}

		if err := EnforceValidationSets(st, decodedAsserts, pinnedSeqs, snaps, ignoreValidation, userID); err != nil {
			return fmt.Errorf("cannot enforce validation sets: %v", err)
		}
	}
	return nil
}

// maybeRestoreValidationSetsAndRevertSnaps restores validation-sets to their
// previous state using validation sets stack if there are any enforced
// validation sets and - if necessary - creates tasksets to revert some or all
// of the refreshed snaps to their previous revisions to satisfy the restored
// validation sets tracking.
var maybeRestoreValidationSetsAndRevertSnaps = func(st *state.State, refreshedSnaps []string, fromChange string) ([]*state.TaskSet, error) {
	enforcedSets, err := EnforcedValidationSets(st)
	if err != nil {
		return nil, err
	}

	if enforcedSets == nil {
		// no enforced validation sets, nothing to do
		return nil, nil
	}

	installedSnaps, ignoreValidation, err := InstalledSnaps(st)
	if err != nil {
		return nil, fmt.Errorf("internal error: cannot get installed snaps: %v", err)
	}

	if err := enforcedSets.CheckInstalledSnaps(installedSnaps, ignoreValidation); err == nil {
		// validation sets are still correct, nothing to do
		logger.Debugf("validation sets are still correct after partial refresh")
		return nil, nil
	}

	// restore previous validation sets tracking state
	if err := RestoreValidationSetsTracking(st); err != nil {
		return nil, fmt.Errorf("cannot restore validation sets: %v", err)
	}

	// no snaps were refreshed, after restoring validation sets tracking
	// there is nothing else to do
	if len(refreshedSnaps) == 0 {
		logger.Debugf("validation set tracking restored, no snaps were refreshed")
		return nil, nil
	}

	// we need to fetch enforced sets again because of RestoreValidationSetsTracking.
	enforcedSets, err = EnforcedValidationSets(st)
	if err != nil {
		return nil, err
	}

	if enforcedSets == nil {
		return nil, fmt.Errorf("internal error: no enforced validation sets after restoring from the stack")
	}

	// check installed snaps again against restored validation-sets.
	// this may fail which is fine, but it tells us which snaps are
	// at invalid revisions and need reverting.
	err = enforcedSets.CheckInstalledSnaps(installedSnaps, ignoreValidation)
	if err == nil {
		// all fine after restoring validation sets: this can happen if previous
		// validation sets only required a snap (regardless of its revision), then
		// after update they require a specific snap revision, so after restoring
		// we are back with the good state.
		logger.Debugf("validation sets still valid after partial refresh, no snaps need reverting")
		return nil, nil
	}
	verr, ok := err.(*snapasserts.ValidationSetsValidationError)
	if !ok {
		return nil, fmt.Errorf("internal error: %v", err)
	}

	if len(verr.WrongRevisionSnaps) == 0 {
		// if we hit ValidationSetsValidationError but it's not about wrong revisions,
		// then something is really broken (we shouldn't have invalid or missing required
		// snaps at this point).
		return nil, fmt.Errorf("internal error: unexpected validation error of installed snaps after unsuccessful refresh: %v", verr)
	}

	wrongRevSnaps := func() []string {
		snaps := make([]string, 0, len(verr.WrongRevisionSnaps))
		for sn := range verr.WrongRevisionSnaps {
			snaps = append(snaps, sn)
		}
		return snaps
	}
	logger.Debugf("refreshed snaps: %s, snaps at wrong revisions: %s", strutil.Quoted(refreshedSnaps), strutil.Quoted(wrongRevSnaps()))

	// revert some or all snaps
	var tss []*state.TaskSet
	for _, snapName := range refreshedSnaps {
		if verr.WrongRevisionSnaps[snapName] != nil {
			// XXX: should we be extra paranoid and use RevertToRevision with
			// the specific revision from verr.WrongRevisionSnaps?
			ts, err := Revert(st, snapName, Flags{RevertStatus: NotBlocked}, fromChange)
			if err != nil {
				return nil, fmt.Errorf("cannot revert snap %q: %v", snapName, err)
			}
			tss = append(tss, ts)
			delete(verr.WrongRevisionSnaps, snapName)
		}
	}

	if len(verr.WrongRevisionSnaps) > 0 {
		return nil, fmt.Errorf("internal error: some snaps were not refreshed but are at wrong revisions: %s", strutil.Quoted(wrongRevSnaps()))
	}

	return tss, nil
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

type dirMigrationOptions struct {
	// UseHidden states whether the user has requested that the hidden data dir be used
	UseHidden bool

	// MigratedToHidden states whether the data has been migrated to the hidden dir
	MigratedToHidden bool

	// MigratedToExposedHome states whether the ~/Snap migration has been done.
	MigratedToExposedHome bool
}

// GetSnapDirOpts returns the snap dir options based on the current the migration status
func (o *dirMigrationOptions) getSnapDirOpts() *dirs.SnapDirOptions {
	return &dirs.SnapDirOptions{HiddenSnapDataDir: o.MigratedToHidden, MigratedToExposedHome: o.MigratedToExposedHome}
}

// GetSnapDirOpts returns the options required to get the correct snap dir.
var GetSnapDirOpts = func(st *state.State, name string) (*dirs.SnapDirOptions, error) {
	var snapst SnapState
	if err := Get(st, name, &snapst); err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}

	hiddenOpts, err := getDirMigrationOpts(st, &snapst, nil)
	if err != nil {
		return nil, err
	}

	return hiddenOpts.getSnapDirOpts(), nil
}

// getDirMigrationOpts checks if the feature flag is set and if the snap data
// has been migrated, first checking the SnapSetup (if not nil) and then
// the SnapState. The state must be locked by the caller.
var getDirMigrationOpts = func(st *state.State, snapst *SnapState, snapsup *SnapSetup) (*dirMigrationOptions, error) {
	tr := config.NewTransaction(st)
	hiddenDir, err := features.Flag(tr, features.HiddenSnapDataHomeDir)
	if err != nil {
		return nil, fmt.Errorf("cannot read feature flag %q: %w", features.HiddenSnapDataHomeDir, err)
	}

	opts := &dirMigrationOptions{UseHidden: hiddenDir}

	if snapst != nil {
		opts.MigratedToHidden = snapst.MigratedHidden
		opts.MigratedToExposedHome = snapst.MigratedToExposedHome
	}

	// it was migrated during this change (might not be in the state yet)
	if snapsup != nil {
		switch {
		case snapsup.MigratedHidden && snapsup.UndidHiddenMigration:
			// should never happen except for programmer error
			return nil, fmt.Errorf("internal error: ~/.snap migration was done and reversed in same change without updating migration flags")
		case snapsup.MigratedHidden:
			opts.MigratedToHidden = true
		case snapsup.UndidHiddenMigration:
			opts.MigratedToHidden = false
		}

		switch {
		case (snapsup.EnableExposedHome || snapsup.MigratedToExposedHome) &&
			(snapsup.DisableExposedHome || snapsup.RemovedExposedHome):
			// should never happen except for programmer error
			return nil, fmt.Errorf("internal error: ~/Snap migration was done and reversed in same change without updating migration flags")
		case snapsup.MigratedToExposedHome:
			fallthrough
		case snapsup.EnableExposedHome:
			opts.MigratedToExposedHome = true
		case snapsup.RemovedExposedHome:
			fallthrough
		case snapsup.DisableExposedHome:
			opts.MigratedToExposedHome = false
		}
	}

	return opts, nil
}
