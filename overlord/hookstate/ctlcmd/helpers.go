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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
)

var finalTasks map[string]bool

var (
	servicestateControl        = servicestate.Control
	snapstateInstallComponents = snapstate.InstallComponents
	snapstateRemoveComponents  = snapstate.RemoveComponents
)

func init() {
	finalTasks = make(map[string]bool, len(snapstate.FinalTasks))
	for _, kind := range snapstate.FinalTasks {
		finalTasks[kind] = true
	}
}

func currentSnapInfo(st *state.State, snapName string) (*snap.Info, error) {
	var snapst snapstate.SnapState
	if err := snapstate.Get(st, snapName, &snapst); err != nil {
		return nil, err
	}

	return snapst.CurrentInfo()
}

func getServiceInfos(st *state.State, snapName string, serviceNames []string) ([]*snap.AppInfo, error) {
	st.Lock()
	defer st.Unlock()

	info, err := currentSnapInfo(st, snapName)
	if err != nil {
		return nil, err
	}

	if len(serviceNames) == 0 {
		// all services
		return info.Services(), nil
	}

	var svcs []*snap.AppInfo
	for _, svcName := range serviceNames {
		if svcName == snapName {
			// all the services
			return info.Services(), nil
		}
		if !strings.HasPrefix(svcName, snapName+".") {
			return nil, fmt.Errorf(i18n.G("unknown service: %q"), svcName)
		}
		// this doesn't support service aliases
		app, ok := info.Apps[svcName[1+len(snapName):]]
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

	// Note: Multiple snaps could be installed in single transaction mode
	// where all snap tasksets are in a single lane.
	// This is non-issue for configure hook since the command tasks are
	// queued at the very end of the change unlike the default-configure
	// hook.
	for _, ts := range tts {
		for _, t := range tasks {
			if finalTasks[t.Kind()] {
				t.WaitAll(ts)
			} else {
				ts.WaitFor(t)
			}
		}
		change.AddAll(ts)
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

func runServiceCommand(context *hookstate.Context, inst *servicestate.Instruction) error {
	if context == nil {
		return &MissingContextError{inst.Action}
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
	chg := st.NewChange("service-control", fmt.Sprintf("Running service command for snap %q", context.InstanceName()))
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

	info, err := currentSnapInfo(st, hctx.InstanceName())
	if err != nil {
		return nil, err
	}
	return snapstateInstallComponents(context.TODO(), st, cmd.components, info,
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

	switch cmd.operation {
	case installManagementCommand:
		tss, err = createSnapctlInstallTasks(hctx, cmd)
		cmdStr = "install"
		cmdVerb = "Installing"
	case removeManagementCommand:
		tss, err = createSnapctlRemoveTasks(hctx, cmd)
		cmdStr = "remove"
		cmdVerb = "Removing"
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
	chg := st.NewChange("snapctl-"+cmdStr,
		fmt.Sprintf("%s components %v for snap %s",
			cmdVerb, cmd.components, hctx.InstanceName()))
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

func jsonRaw(v interface{}) *json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Errorf("internal error: cannot marshal attributes: %v", err))
	}
	raw := json.RawMessage(data)
	return &raw
}

// getAttribute unmarshals into result the value of the provided key from attributes map.
// If the key does not exist, an error of type *NoAttributeError is returned.
// The provided key may be formed as a dotted key path through nested maps.
// For example, the "a.b.c" key describes the {a: {b: {c: value}}} map.
func getAttribute(snapName string, subkeys []string, pos int, attrs map[string]interface{}, result interface{}) error {
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

	attrsm, ok := value.(map[string]interface{})
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
