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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/jsonutil"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

var finalTasks map[string]bool

func init() {
	finalTasks = make(map[string]bool, len(snapstate.FinalTasks))
	for _, kind := range snapstate.FinalTasks {
		finalTasks[kind] = true
	}
}

func getServiceInfos(st *state.State, snapName string, serviceNames []string) ([]*snap.AppInfo, error) {
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(st, snapName, &snapst))

	info := mylog.Check2(snapst.CurrentInfo())

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

var servicestateControl = servicestate.Control

func queueCommand(context *hookstate.Context, tts []*state.TaskSet) error {
	hookTask, ok := context.Task()
	if !ok {
		return fmt.Errorf("attempted to queue command with ephemeral context")
	}

	st := context.State()
	st.Lock()
	defer st.Unlock()

	change := hookTask.Change()
	hookTaskLanes := hookTask.Lanes()
	tasks := change.LaneTasks(hookTaskLanes...)

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

	for _, ts := range tts {
		for _, t := range tasks {
			// queue service command after all tasks, except for final tasks which must come after service commands
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

func runServiceCommand(context *hookstate.Context, inst *servicestate.Instruction) error {
	if context == nil {
		return &MissingContextError{inst.Action}
	}

	st := context.State()
	appInfos := mylog.Check2(getServiceInfos(st, context.InstanceName(), inst.Names))

	flags := &servicestate.Flags{CreateExecCommandTasks: true}
	// passing context so we can ignore self-conflicts with the current change
	st.Lock()
	tts := mylog.Check2(servicestateControl(st, appInfos, inst, nil, flags, context))
	st.Unlock()

	if !context.IsEphemeral() && context.HookName() == "configure" {
		return queueCommand(context, tts)
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
	data := mylog.Check2(json.Marshal(v))

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
		mylog.Check(jsonutil.DecodeWithNumber(bytes.NewReader(*raw), &result))

		return nil
	}

	attrsm, ok := value.(map[string]interface{})
	if !ok {
		raw, ok := value.(*json.RawMessage)
		if !ok {
			raw = jsonRaw(value)
		}
		mylog.Check(jsonutil.DecodeWithNumber(bytes.NewReader(*raw), &attrsm))

	}
	return getAttribute(snapName, subkeys, pos+1, attrsm, result)
}
