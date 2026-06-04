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
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/timings"
	"gopkg.in/tomb.v2"
)

// timeout for tasks to check if the prerequisites are ready
var prerequisitesRetryTimeout = 10 * time.Second

func (m *SnapManager) doPrerequisites(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	tm := state.TimingsForTask(t)
	defer tm.Save(st)

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

	// If transactional, use a single lane for all tasks, so when one fails the
	// changes for all affected snaps will be undone. Otherwise, have different
	// lanes per snap so failures only affect the culprit snap.
	flags := Flags{
		Transaction: snapsup.Transaction,

		// TODO: as a temporary workaround for a bug that occurs when a snap updates
		// a prereq, we disable rerefreshes.
		//
		// specifically, if the snap that pulls in the prereq contains a configure
		// hook that creates some tasks via snapctl, then those tasks will end up
		// waiting on the check-rerefresh task for the updated prereq. the
		// check-rerefresh task panics if any tasks are found to be waiting on it.
		NoReRefresh: true,

		// we're calling an API facing call which would otherwise be normally
		// expected to produce a delayed effects taskset, but since the desire
		// is to inject the tasksets into the current change, set the flag to
		// avoid generating one
		NoDelayedSideEffects: true,
	}
	if flags.Transaction == client.TransactionAllSnaps {
		lanes := t.Lanes()
		if len(lanes) != 1 {
			return fmt.Errorf("internal error: more than one lane (%d) on a transactional action", len(lanes))
		}

		flags.Lane = lanes[0]
	} else {
		flags.Transaction = client.TransactionPerSnap
	}

	dctx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return err
	}

	if err := installPrereqs(t, base, snapsup.PrereqContentAttrs, tm, Options{
		Flags:     flags,
		UserID:    snapsup.UserID,
		DeviceCtx: dctx,
		ConflictOptions: ConflictOptions{
			FromChange: t.Change().ID(),
			// setting this lets us use snap update conflict detection, even
			// though we're passing in the change ID
			DoNotIgnoreFromChangeInTaskConflictCheck: true,
		},
	}); err != nil {
		return err
	}

	return nil
}

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

func installPrereqs(t *state.Task, base string, prereq map[string][]string, tm timings.Measurer, opts Options) error {
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
			ts, err = installOneBaseOrRequired(t, prereqName, contentAttrs, defaultPrereqSnapsChannel(), onInFlightErr, opts)
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
			// base prerequisites are installed with the same options as other
			// prerequisites, except that they must be verified to have type
			// base.
			opts := opts
			opts.Flags.RequireTypeBase = true

			tsBase, err = installOneBaseOrRequired(t, base, nil, defaultBaseSnapsChannel(), onInFlightErr, opts)
		})
		if err != nil {
			return prereqError("snap base", base, err)
		}
	}

	var tsSnapd *state.TaskSet
	installSnapd, err := considerSnapdAsPrereq(st)
	if err != nil {
		return err
	}
	if installSnapd {
		timings.Run(tm, "install-prereq", "install snapd", func(timings.Measurer) {
			tsSnapd, err = installOneBaseOrRequired(t, "snapd", nil, defaultSnapdSnapsChannel(), onInFlightErr, opts)
		})
		if err != nil {
			return prereqError("system snap", "snapd", err)
		}
	}

	chg := t.Change()
	// add all required snaps, no ordering, this will be done in the
	// auto-connect task handler
	for _, ts := range tss {
		chg.AddAll(ts)
	}
	// add the base if needed, prereqs else must wait on this
	if tsBase != nil {
		for _, t := range chg.Tasks() {
			t.WaitAll(tsBase)
		}
		chg.AddAll(tsBase)
	}
	// add snapd if needed, everything must wait on this
	if tsSnapd != nil {
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

// considerSnapdAsPrereq returns true if we should install snapd as a
// prerequisite. Returns true on classic systems that are already seeded. Not
// allowed on Ubuntu Core systems, this requires remodeling.
func considerSnapdAsPrereq(st *state.State) (bool, error) {
	installed, err := isInstalled(st, "snapd")
	if err != nil {
		return false, err
	}

	// consider the state of seeding to avoid seed conflict error
	var seeded bool
	if err := st.Get("seeded", &seeded); err != nil && !errors.Is(err, state.ErrNoState) {
		return false, err
	}

	return release.OnClassic && seeded && !installed, nil
}

func installOneBaseOrRequired(t *state.Task, snapName string, contentAttrs []string, channel string, onInFlight error, opts Options) (*state.TaskSet, error) {
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

	shouldWaitForInFlightInstall := func(snapName string) (bool, error) {
		linkTask, err := findLinkSnapTaskForSnap(st, snapName)
		if err != nil {
			return false, err
		}

		if linkTask == nil {
			// snap is not being installed
			return false, nil
		}

		if opts.Flags.RequireTypeBase {
			// if this snap is already ordered behind the in-flight base refresh
			// prerequisites does not need to wait for that base out-of-band as
			// well.
			alreadyOrdered, err := snapWaitsForBaseLinkInSameLane(t, linkTask)
			if err != nil {
				return false, err
			}

			if alreadyOrdered {
				return false, nil
			}
		}

		if onInFlight != nil && willWaitOn(linkTask, t) {
			return false, fmt.Errorf(
				"internal error: prerequisites task cannot wait on task %[1]q because task %[1]q is waiting on the prerequisites task",
				linkTask.ID(),
			)
		}

		// snap is being installed, retry later
		return true, nil
	}

	// if we are remodeling, then we should return early due to the way that
	// tasks are ordered by the remodeling code. specifically, all snap
	// downloads during a remodel happen prior to snap installation. thus,
	// we cannot wait for snaps to be installed here. see remodelTasks for
	// more information on how the tasks are ordered.
	if opts.DeviceCtx.ForRemodeling() {
		return nil, nil
	}

	if isInstalled {
		if len(contentAttrs) > 0 {
			// the default provider is already installed, update it if it's missing content attributes the snap needs
			return updatePrereqIfOutdated(t, snapName, contentAttrs, opts)
		}

		// other kind of dependency, check if it's in progress
		if ok, err := shouldWaitForInFlightInstall(snapName); err != nil {
			return nil, err
		} else if ok {
			return nil, onInFlight
		}

		return nil, nil
	}

	// not installed, wait for it if it is. If not, we'll install it
	if ok, err := shouldWaitForInFlightInstall(snapName); err != nil {
		return nil, err
	} else if ok {
		return nil, onInFlight
	}

	// not installed, nor queued for install -> install it
	_, ts, err := InstallOne(context.TODO(), st, StoreInstallGoal(StoreSnap{
		InstanceName: snapName,
		RevOpts: RevisionOptions{
			Channel: channel,
		},
	}), opts)

	// something might have triggered an explicit install while
	// the state was unlocked -> deal with that here by simply
	// retrying the operation.
	var conflErr *ChangeConflictError
	if errors.As(err, &conflErr) {
		// conflicted with an install in the same change, just skip
		if conflErr.ChangeID == t.Change().ID() {
			return nil, nil
		}

		return nil, &state.Retry{After: prerequisitesRetryTimeout}
	}
	return ts, err
}

// updates a prerequisite, if it's not providing a content interface that a plug expects it to
func updatePrereqIfOutdated(t *state.Task, snapName string, contentAttrs []string, opts Options) (*state.TaskSet, error) {
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

	// default provider is missing some content tags (likely outdated) so update it
	ts, err := UpdateOne(context.Background(), st, StoreUpdateGoal(StoreUpdate{
		InstanceName: snapName,
	}), nil, opts)
	if err != nil {
		if conflErr, ok := err.(*ChangeConflictError); ok {
			// If we aren't seeded, then it's too early to do any updates and we cannot
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

	if err := maybeMergeLateSeedRefreshPrereq(t.Change(), opts.DeviceCtx, ts); err != nil {
		return nil, err
	}

	return ts, nil
}

// shouldSkipToAvoidConflict checks for conflicting tasks. Returns true if the
// operation should be skipped. The error can be a state.Retry if the operation
// should be retried later.
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

// hasAllContentAttrs checks if the snap has slots with "content" attributes
// matching the ones that the snap being installed requires
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

func instanceNameFromTask(t *state.Task) (string, bool) {
	snapsup, err := TaskSnapSetup(t)
	if err != nil {
		return "", false
	}
	return snapsup.InstanceName(), true
}

func isInstalled(st *state.State, snapName string) (bool, error) {
	var snapState SnapState
	err := Get(st, snapName, &snapState)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return false, err
	}
	return snapState.IsInstalled(), nil
}

func prereqError(what, snapName string, err error) error {
	if _, ok := err.(*state.Retry); ok {
		return err
	}
	return fmt.Errorf("cannot install %s %q: %v", what, snapName, err)
}

func maybeFindTaskInChangeForSnap(chg *state.Change, kind, snapName string) (*state.Task, error) {
	for _, t := range chg.Tasks() {
		if t.Status().Ready() || t.Kind() != kind {
			continue
		}

		snapsup, err := TaskSnapSetup(t)
		if err != nil {
			return nil, err
		}
		if snapsup.InstanceName() == snapName {
			return t, nil
		}
	}

	return nil, nil
}

func findLinkSnapTaskForSnap(st *state.State, snapName string) (*state.Task, error) {
	for _, chg := range st.Changes() {
		if chg.IsReady() {
			continue
		}

		t, err := maybeFindTaskInChangeForSnap(chg, "link-snap", snapName)
		if err != nil {
			return nil, err
		}

		if t != nil {
			return t, nil
		}
	}

	return nil, nil
}

// willWaitOn returns true if graph waits (directly or transitively) on target.
func willWaitOn(graph *state.Task, target *state.Task) bool {
	seen := make(map[string]bool)
	queue := append([]*state.Task(nil), graph.WaitTasks()...)
	for i := 0; i < len(queue); i++ {
		current := queue[i]
		if seen[current.ID()] {
			continue
		}

		seen[current.ID()] = true
		if current.ID() == target.ID() {
			return true
		}

		for _, child := range current.WaitTasks() {
			if !seen[child.ID()] {
				queue = append(queue, child)
			}
		}
	}

	return false
}

// tasksShareLane reports whether the two tasks share at least one lane.
func tasksShareLane(t, other *state.Task) bool {
	lanes := make(map[int]bool, len(t.Lanes()))
	for _, lane := range t.Lanes() {
		lanes[lane] = true
	}

	for _, lane := range other.Lanes() {
		if lanes[lane] {
			return true
		}
	}

	return false
}

// snapWaitsForBaseLinkInSameLane reports whether another task for the same snap
// in the same lane is already ordered behind the base's link-snap task.
func snapWaitsForBaseLinkInSameLane(prereqs *state.Task, baseLink *state.Task) (bool, error) {
	// if they don't share a change, then there won't be dependencies already
	// established
	if prereqs.Change().ID() != baseLink.Change().ID() {
		return false, nil
	}

	chg := prereqs.Change()

	instanceName, ok := instanceNameFromTask(prereqs)
	if !ok {
		return false, errors.New("internal error: cannot find instance name on prerequisites task")
	}

	for _, t := range chg.Tasks() {
		if t.ID() == prereqs.ID() {
			continue
		}

		if !tasksShareLane(prereqs, t) {
			continue
		}

		other, ok := instanceNameFromTask(t)
		if !ok || other != instanceName {
			continue
		}

		// this check could be made stronger by enforcing that the first local
		// modification task for the snap waits on the base's link-snap task,
		// but we don't have a great way to find that task at this point in
		// time, since we don't have access to edges any more.
		//
		// in short, this is somewhat of a heuristic. we'd need to enumerate all
		// before-local-modification tasks if we want to make this check better.
		if willWaitOn(t, baseLink) {
			return true, nil
		}
	}

	return false, nil
}
