// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package ctlcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/jsonutil"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/swfeats"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
)

var finalTasks map[string]bool

var (
	servicestateControl        = servicestate.Control
	snapstateInstallComponents = snapstate.InstallComponents
	snapstateRemoveComponents  = snapstate.RemoveComponents
)

var timeAfter = time.After

var (
	serviceControlChangeKind = swfeats.RegisterChangeKind("service-control")
	snapctlInstallChangeKind = swfeats.RegisterChangeKind("snapctl-install")
	snapctlRemoveChangeKind  = swfeats.RegisterChangeKind("snapctl-remove")
)

func init() {
	finalTasks = make(map[string]bool, len(snapstate.FinalTasks))
	for _, kind := range snapstate.FinalTasks {
		finalTasks[kind] = true
	}
}

const snapctlDebounceWindow = 200 * time.Millisecond

// finalSeedTask is the last task that should run during seeding. This is used
// in the special handling of the "seed" change, which requires that we
// introspect the change for this specific task. Finding this task allows us to
// properly organize the hook tasks in the chain of tasks in the change.
const finalSeedTask = "mark-seeded"

func currentSnapInfo(st *state.State, snapInstance string) (*snap.Info, error) {
	var snapst snapstate.SnapState
	if err := snapstate.Get(st, snapInstance, &snapst); err != nil {
		return nil, err
	}

	return snapst.CurrentInfo()
}

func getServiceInfos(st *state.State, snapInstance string, serviceNames []string) ([]*snap.AppInfo, error) {
	st.Lock()
	defer st.Unlock()

	info, err := currentSnapInfo(st, snapInstance)
	if err != nil {
		return nil, err
	}

	if len(serviceNames) == 0 {
		// all services
		return info.Services(), nil
	}

	var svcs []*snap.AppInfo
	for _, svcName := range serviceNames {
		if svcName == snapInstance {
			// implicit all services
			return info.Services(), nil
		}
		if !strings.HasPrefix(svcName, snapInstance+".") {
			return nil, fmt.Errorf(i18n.G("unknown service: %q"), svcName)
		}
		// this doesn't support service aliases
		app, ok := info.Apps[svcName[1+len(snapInstance):]]
		if !(ok && app.IsService()) {
			return nil, fmt.Errorf(i18n.G("unknown service: %q"), svcName)
		}
		svcs = append(svcs, app)
	}

	return svcs, nil
}

func prepareQueueCommand(context *hookstate.Context, tts []*state.TaskSet) (change *state.Change, tasks []*state.Task, err error) {
	hookTask, ok := context.Task()
	if !ok {
		return nil, nil, fmt.Errorf("attempted to queue command with ephemeral context")
	}

	change = hookTask.Change()
	hookTaskLanes := hookTask.Lanes()
	tasks = change.LaneTasks(hookTaskLanes...)

	// When installing or updating multiple snaps, there is one lane per snap.
	// We want service command to join respective lane (it's the lane the hook belongs to).
	// In case there are no lanes, only the default lane no. 0, there is no need to join it.
	if len(hookTaskLanes) == 1 && hookTaskLanes[0] == 0 {
		hookTaskLanes = nil
	}
	for _, l := range hookTaskLanes {
		for _, ts := range tts {
			ts.JoinLane(l)
		}
	}

	return change, tasks, err
}

// queueCommand queues service command after all tasks, except for final tasks which must come after service commands.
func queueCommand(context *hookstate.Context, tts []*state.TaskSet) error {
	st := context.State()
	st.Lock()
	defer st.Unlock()

	change, tasks, err := prepareQueueCommand(context, tts)
	if err != nil {
		return err
	}

	if change.Kind() != "seed" {
		// Note: Multiple snaps could be installed in single transaction mode
		// where all snap tasksets are in a single lane.
		// This is non-issue for configure hook since the command tasks are
		// queued at the very end of the change unlike the default-configure
		// hook.
		for _, ts := range tts {
			for _, t := range tasks {
				if finalTasks[t.Kind()] {
					t.WaitAll(ts)
				} else if t.Kind() == "process-delayed-security-backend-effects" || t.Kind() == "check-rerefresh" {
					// do not wait for freestanding tasks
				} else {
					ts.WaitFor(t)
				}
			}
			change.AddAll(ts)
		}
	} else {
		// as a special case, we handle the seeding change slightly differently.
		// we must look at all tasks for the "mark-seeded" task, without
		// considering lanes. this is because seeding uses lanes to put
		// essential snaps and non-essential snaps in separate lanes, but the
		// mark-seeded task isn't in a lane with them.
		for _, ts := range tts {
			for _, t := range change.Tasks() {
				if t.Kind() == finalSeedTask {
					t.WaitAll(ts)
				} else {
					ts.WaitFor(t)
				}
			}
			change.AddAll(ts)
		}
	}

	// As this can be run from what was originally the last task of a change,
	// make sure the tasks added to the change are considered immediately.
	st.EnsureBefore(0)

	return nil
}

// queueDefaultConfigureHookCommand queues service command exactly after start-snap-services.
//
// This is possible because the default-configure hook is run on first-install only and right
// after start-snap-services is the nearest we can queue the service commands safely to make
// sure all the needed state is setup properly.
func queueDefaultConfigureHookCommand(context *hookstate.Context, tts []*state.TaskSet) error {
	st := context.State()
	st.Lock()
	defer st.Unlock()

	_, tasks, err := prepareQueueCommand(context, tts)
	if err != nil {
		return err
	}

	for _, t := range tasks {
		if t.Kind() == "start-snap-services" {
			snapsup, err := snapstate.TaskSnapSetup(t)
			if err != nil {
				return err
			}
			// Multiple snaps could be installed in single transaction mode
			// where all snap tasksets are in a single lane.
			// Check that the task belongs to the relevant snap.
			if snapsup.InstanceName() != context.InstanceName() {
				continue
			}
			for _, ts := range tts {
				snapstate.InjectTasks(t, ts)
			}
			break
		}
	}

	// As this can be run from what was originally the last task of a change,
	// make sure the tasks added to the change are considered immediately.
	st.EnsureBefore(0)

	return nil
}

func maybePatchServiceNames(snapInstance string, serviceNames []string) (
	updatedServiceNames []string, patched bool, err error,
) {
	snapName, snapInstanceKey := snap.SplitInstanceName(snapInstance)
	hasInstanceKey := snapInstanceKey != ""

	if !hasInstanceKey {
		// no patching needed, return names as they are
		return serviceNames, false, nil
	}

	// Backward compatibility path for a scenario when snapctl service operation
	// is called in a context of a snap with instance key. It is possible that
	// the request uses service names of form 'snap.app' which does not include
	// an instance key. We want to 'patch' them to 'snap_foo.app', such that
	// existing snaps that aren't completely aware of parallel installs work
	// correctly.

	updatedServiceNames = make([]string, 0, len(serviceNames))
	// Count of service names which included an instance key in their snap name.
	// We can only do the patching if either all service names had an instance
	// key, in which case the names aren't changed, or none of them and so the
	// names were fixed up as needed.
	withInstanceKeyCnt := 0
	for _, svcN := range serviceNames {
		if svcN == snapName {
			// same as base snap name (without instance key), a short hand
			// syntax for restart all services of a snap
			updatedServiceNames = append(updatedServiceNames, snapInstance)
			patched = true
			continue
		}

		svcSnapInstanceName, svcApp := snap.SplitSnapApp(svcN)
		svcSnapName, svcSnapInstanceKey := snap.SplitInstanceName(svcSnapInstanceName)

		if svcSnapName == snapName {
			// only apply patching if the snap name matches

			if svcSnapInstanceKey == "" {
				// snap name used in the full service name does not include instance
				// key, needs patching
				updatedServiceNames = append(updatedServiceNames, snap.JoinSnapApp(snapInstance, svcApp))
				patched = true
				continue
			}

			withInstanceKeyCnt++

			if svcSnapInstanceKey != snapInstanceKey {
				return nil, false, fmt.Errorf(i18n.G("unexpected snap instance key: %q"), svcSnapInstanceKey)
			}
		}

		updatedServiceNames = append(updatedServiceNames, svcN)
	}

	if withInstanceKeyCnt != 0 && withInstanceKeyCnt != len(serviceNames) {
		return nil, false, fmt.Errorf(i18n.G("inconsistent use of snap instance key"))
	}

	return updatedServiceNames, patched, nil
}

func runServiceCommand(context *hookstate.Context, inst *servicestate.Instruction) error {
	if context == nil {
		return &MissingContextError{inst.Action}
	}

	// patch service names for parallel installed snap if needed
	var err error
	inst.Names, _, err = maybePatchServiceNames(context.InstanceName(), inst.Names)
	if err != nil {
		return err
	}

	st := context.State()
	appInfos, err := getServiceInfos(st, context.InstanceName(), inst.Names)
	if err != nil {
		return err
	}

	flags := &servicestate.Flags{CreateExecCommandTasks: true}
	// passing context so we can ignore self-conflicts with the current change
	st.Lock()
	tts, err := servicestateControl(st, appInfos, inst, nil, flags, context)
	st.Unlock()
	if err != nil {
		return err
	}

	if !context.IsEphemeral() {
		// queue service command for default-configure and configure hooks.
		switch context.HookName() {
		case "configure":
			return queueCommand(context, tts)
		case "default-configure":
			return queueDefaultConfigureHookCommand(context, tts)
		}
	}

	st.Lock()
	chg := st.NewChange(serviceControlChangeKind, fmt.Sprintf("Running service command for snap %q", context.InstanceName()))
	for _, ts := range tts {
		chg.AddAll(ts)
	}
	st.EnsureBefore(0)
	st.Unlock()

	select {
	case <-chg.Ready():
		st.Lock()
		defer st.Unlock()
		return chg.Err()
	case <-time.After(configstate.ConfigureHookTimeout() / 2):
		return fmt.Errorf("%s command is taking too long", inst.Action)
	}
}

func validateSnapAndCompsNames(names []string, ctxSnap string) ([]string, error) {
	var allComps []string
	for _, name := range names {
		snap, comps := snap.SplitSnapInstanceAndComponents(name)
		// if snap is present it must be the context snap for the moment
		if snap != "" && snap != ctxSnap {
			return nil, errors.New("cannot install snaps using snapctl")
		}
		for _, comp := range comps {
			if err := naming.ValidateSnap(comp); err != nil {
				return nil, err
			}
		}
		allComps = append(allComps, comps...)
	}
	return allComps, nil
}

type managementCommandOp int

const (
	installManagementCommand managementCommandOp = iota
	removeManagementCommand
)

type managementCommand struct {
	operation  managementCommandOp
	components []string
}

func changeIDIfNotEphemeral(hctx *hookstate.Context) string {
	if !hctx.IsEphemeral() {
		return hctx.ChangeID()
	}
	return ""
}

func createSnapctlInstallTasks(hctx *hookstate.Context, cmd managementCommand) (tss []*state.TaskSet, err error) {
	st := hctx.State()
	st.Lock()
	defer st.Unlock()

	// note, vsets might be nil if no validation sets are going to be enforced
	// by the current change
	vsets, err := hctx.PendingValidationSets()
	if err != nil {
		return nil, err
	}

	info, err := currentSnapInfo(st, hctx.InstanceName())
	if err != nil {
		return nil, err
	}
	return snapstateInstallComponents(context.TODO(), st, cmd.components, info, vsets,
		snapstate.Options{ExpectOneSnap: true, FromChange: changeIDIfNotEphemeral(hctx)})
}

func createSnapctlRemoveTasks(hctx *hookstate.Context, cmd managementCommand) (tss []*state.TaskSet, err error) {
	st := hctx.State()
	st.Lock()
	defer st.Unlock()

	return snapstateRemoveComponents(st, hctx.InstanceName(), cmd.components,
		snapstate.RemoveComponentsOpts{RefreshProfile: true,
			FromChange: changeIDIfNotEphemeral(hctx)})
}

func runSnapManagementCommand(hctx *hookstate.Context, cmd managementCommand) error {
	st := hctx.State()
	var tss []*state.TaskSet
	var err error
	var cmdStr, cmdVerb string

	var changeKind string
	switch cmd.operation {
	case installManagementCommand:
		tss, err = createSnapctlInstallTasks(hctx, cmd)
		cmdStr = "install"
		cmdVerb = "Installing"
		changeKind = snapctlInstallChangeKind
	case removeManagementCommand:
		tss, err = createSnapctlRemoveTasks(hctx, cmd)
		cmdStr = "remove"
		cmdVerb = "Removing"
		changeKind = snapctlRemoveChangeKind
	default:
		err = fmt.Errorf("internal error: %q is not a valid snap management command", cmd.operation)
	}
	if err != nil {
		return err
	}

	if !hctx.IsEphemeral() {
		// Differently to service control commands, we always queue the
		// management tasks if run from a hook.
		return queueCommand(hctx, tss)
	}

	st.Lock()
	chg := st.NewChange(changeKind,
		fmt.Sprintf("%s components %v for snap %s",
			cmdVerb, cmd.components, hctx.InstanceName()))
	chg.Set("initiated-by-snap", hctx.InstanceName())
	for _, ts := range tss {
		chg.AddAll(ts)
	}
	st.EnsureBefore(0)
	st.Unlock()

	select {
	case <-chg.Ready():
		st.Lock()
		defer st.Unlock()
		return chg.Err()
	case <-time.After(10 * time.Minute):
		return fmt.Errorf("snapctl %s command is taking too long", cmdStr)
	}
}

// NoAttributeError indicates that an interface attribute is not set.
type NoAttributeError struct {
	Attribute string
}

func (e *NoAttributeError) Error() string {
	return fmt.Sprintf("no %q attribute", e.Attribute)
}

// isNoAttribute returns whether the provided error is a *NoAttributeError.
func isNoAttribute(err error) bool {
	_, ok := err.(*NoAttributeError)
	return ok
}

func jsonRaw(v any) *json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Errorf("internal error: cannot marshal attributes: %v", err))
	}
	raw := json.RawMessage(data)
	return &raw
}

type changeRateLimitKey struct {
	ChangeID string
}

// isReady checks if the change is ready, if it is, it returns the status, otherwise state.DoingStatus.
func isReady(hctx *hookstate.Context, changeID string) (state.Status, error) {
	callerSnapName := hctx.InstanceName()

	st := hctx.State()
	st.Lock()
	defer st.Unlock()

	chg := st.Change(changeID)

	if chg == nil {
		return state.DefaultStatus, fmt.Errorf("change %q not found", changeID)
	}

	var initiatorSnapName string
	err := chg.Get("initiated-by-snap", &initiatorSnapName)
	if err != nil {
		return state.DefaultStatus, fmt.Errorf("change %q not found", changeID)
	}

	if initiatorSnapName != callerSnapName {
		return state.DefaultStatus, fmt.Errorf("change %q not found", changeID)
	}

	wait, err := rateLimit(st, changeID, snapctlDebounceWindow)
	if err != nil {
		return state.DefaultStatus, err
	}

	return unlockAndWaitForStatus(st, chg, wait), nil
}

// unlockAndWaitForStatus unlocks the state and waits for the change to be ready.
// The lock must be held prior to calling, and will be re-acquired before returning.
// Returns doingStatus if the change is still in progress, otherwise returns the final
// status of the change.
func unlockAndWaitForStatus(st *state.State, chg *state.Change, wait time.Duration) state.Status {
	st.Unlock()
	// note: we cannot defer the re-lock, since we must re-lock prior to
	// calculating the return value in some branches.

	ready := chg.Ready()

	// The check ensures that both select cases aren't true immediately.
	if wait <= 0 {
		select {
		// use default so the channel is prioritized.
		case <-ready:
			st.Lock()
			return chg.Status()
		default:
			st.Lock()
			return state.DoingStatus
		}
	}

	// Because the wait could've been > 0, the last select between a closed ready channel
	// and a timer.After channel would've be racy.
	select {
	case <-ready:
	case <-timeAfter(wait):
		st.Lock()
		return state.DoingStatus
	}

	st.Lock()
	return chg.Status()
}

// rateLimit returns the amount of time that should be waited before accessing
// this change via snapctl. Internally, data associated with the change is
// cached so that all access to the change shares the same rate limit.
// The lock must be acquired before calling, as it modifies the state object.
func rateLimit(st *state.State, changeID string, rate time.Duration) (wait time.Duration, err error) {
	now := time.Now()

	accessed, err := changeAccessedAt(st, changeID)
	if err != nil {
		return 0, err
	}

	// first time through, we just set the change access to now. next request
	// must wait at least "rate" duration before access.
	if accessed.IsZero() {
		setChangeAccessedAt(st, now, changeID)
		return 0, nil
	}

	durationSinceLastAccess := now.Sub(accessed)

	// user waited on their own, no waiting needed. next access will require
	// waiting at least "rate" duration.
	if durationSinceLastAccess >= rate {
		setChangeAccessedAt(st, now, changeID)
		return 0, nil
	}

	// user needs to wait a bit still. note that durationSinceLastAccess might
	// be negative, since "accessed" could be in the future. this can happen
	// when there are multiple requests in parallel, within a duration less than
	// "rate".
	wait = rate - durationSinceLastAccess

	// current request must wait. next request must wait this amount of time,
	// plus at least "rate" duration.
	setChangeAccessedAt(st, now.Add(wait), changeID)

	return wait, nil
}

func changeAccessedAt(st *state.State, changeID string) (time.Time, error) {
	key := changeRateLimitKey{ChangeID: changeID}
	accessedAt := st.Cached(key)
	if accessedAt == nil {
		return time.Time{}, nil
	}

	accessedNano, ok := accessedAt.(int64)
	if !ok {
		return time.Time{}, fmt.Errorf("error: invalid type (%T) for access time", accessedAt)
	}

	return time.Unix(0, accessedNano), nil
}

func setChangeAccessedAt(st *state.State, accessed time.Time, changeID string) {
	key := changeRateLimitKey{ChangeID: changeID}
	st.Cache(key, accessed.UnixNano())
}

// getAttribute unmarshals into result the value of the provided key from attributes map.
// If the key does not exist, an error of type *NoAttributeError is returned.
// The provided key may be formed as a dotted key path through nested maps.
// For example, the "a.b.c" key describes the {a: {b: {c: value}}} map.
func getAttribute(snapName string, subkeys []string, pos int, attrs map[string]any, result any) error {
	if pos >= len(subkeys) {
		return fmt.Errorf("internal error: invalid subkeys index %d for subkeys %q", pos, subkeys)
	}
	value, ok := attrs[subkeys[pos]]
	if !ok {
		return &NoAttributeError{Attribute: strings.Join(subkeys[:pos+1], ".")}
	}

	if pos+1 == len(subkeys) {
		raw, ok := value.(*json.RawMessage)
		if !ok {
			raw = jsonRaw(value)
		}
		if err := jsonutil.DecodeWithNumber(bytes.NewReader(*raw), &result); err != nil {
			key := strings.Join(subkeys, ".")
			return fmt.Errorf("internal error: cannot unmarshal snap %s attribute %q into %T: %s, json: %s", snapName, key, result, err, *raw)
		}
		return nil
	}

	attrsm, ok := value.(map[string]any)
	if !ok {
		raw, ok := value.(*json.RawMessage)
		if !ok {
			raw = jsonRaw(value)
		}
		if err := jsonutil.DecodeWithNumber(bytes.NewReader(*raw), &attrsm); err != nil {
			return fmt.Errorf("snap %q attribute %q is not a map", snapName, strings.Join(subkeys[:pos+1], "."))
		}
	}
	return getAttribute(snapName, subkeys, pos+1, attrsm, result)
}
