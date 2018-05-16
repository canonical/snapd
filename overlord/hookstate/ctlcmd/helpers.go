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
	"fmt"
	"strings"
	"time"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func getServiceInfos(st *state.State, snapName string, serviceNames []string) ([]*snap.AppInfo, error) {
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	if err := snapstate.Get(st, snapName, &snapst); err != nil {
		return nil, err
	}

	info, err := snapst.CurrentInfo()
	if err != nil {
		return nil, err
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
		ts.WaitAll(state.NewTaskSet(tasks...))
		change.AddAll(ts)
	}
	// As this can be run from what was originally the last task of a change,
	// make sure the tasks added to the change are considered immediately.
	st.EnsureBefore(0)

	return nil
}

func runServiceCommand(context *hookstate.Context, inst *servicestate.Instruction, serviceNames []string) error {
	if context == nil {
		return fmt.Errorf(i18n.G("cannot %s without a context"), inst.Action)
	}

	st := context.State()
	appInfos, err := getServiceInfos(st, context.SnapName(), serviceNames)
	if err != nil {
		return err
	}

	// passing context so we can ignore self-conflicts with the current change
	tts, err := servicestateControl(st, appInfos, inst, context)
	if err != nil {
		return err
	}

	if !context.IsEphemeral() && context.HookName() == "configure" {
		return queueCommand(context, tts)
	}

	st.Lock()
	chg := st.NewChange("service-control", fmt.Sprintf("Running service command for snap %q", context.SnapName()))
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
