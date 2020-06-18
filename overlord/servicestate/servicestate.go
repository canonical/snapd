// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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

package servicestate

import (
	"fmt"
	"time"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/cmdstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type Instruction struct {
	Action string   `json:"action"`
	Names  []string `json:"names"`
	client.StartOptions
	client.StopOptions
	client.RestartOptions
}

type ServiceActionConflictError struct{ error }

// changeIDForContext returns change ID for non-ephemeral context
// or empty string if context is nil or ephemeral.
func changeIDForContext(context *hookstate.Context) string {
	if context != nil && !context.IsEphemeral() {
		if task, ok := context.Task(); ok {
			if chg := task.Change(); chg != nil {
				return chg.ID()
			}
		}
	}
	return ""
}

func ServiceControlMany(st *state.State, appInfos []*snap.AppInfo, inst *Instruction, context *hookstate.Context) ([]*state.TaskSet, error) {
	servicesBySnap := make(map[string][]string)
	for _, app := range appInfos {
		if !app.IsService() {
			// this function should be called with services only
			return nil, fmt.Errorf("internal error: %s is not a service", app.Name)
		}
		snapName := app.Snap.InstanceName()
		servicesBySnap[snapName] = append(servicesBySnap[snapName], app.Name)
	}

	ts := state.NewTaskSet()
	for snapName, services := range servicesBySnap {
		task, err := ServiceControl(st, snapName, services, inst, context)
		if err != nil {
			return nil, err
		}
		// XXX: should we chain tasks here?
		ts.AddTask(task)
	}
	return []*state.TaskSet{ts}, nil
}

func ServiceControl(st *state.State, snapName string, serviceNames []string, inst *Instruction, context *hookstate.Context) (*state.Task, error) {
	st.Lock()
	defer st.Unlock()

	ignoreChangeID := changeIDForContext(context)
	if err := snapstate.CheckChangeConflictMany(st, []string{snapName}, ignoreChangeID); err != nil {
		return nil, &ServiceActionConflictError{err}
	}

	var snapst snapstate.SnapState
	if err := snapstate.Get(st, snapName, &snapst); err != nil {
		return nil, err
	}
	info, err := snapst.CurrentInfo()
	if err != nil {
		return nil, err
	}
	if len(info.Services()) == 0 {
		return nil, fmt.Errorf("snap %q does not have services", snapName)
	}

	cmd := &ServiceAction{SnapName: snapName}
	switch {
	case inst.Action == "start":
		cmd.Action = "start"
		if inst.Enable {
			cmd.ActionModifier = "enable"
		}
	case inst.Action == "stop":
		cmd.Action = "stop"
		if inst.Disable {
			cmd.ActionModifier = "disable"
		}
	case inst.Action == "restart":
		if inst.Reload {
			cmd.Action = "reload-or-restart"
		} else {
			cmd.Action = "restart"
		}
	default:
		return nil, fmt.Errorf("unknown action %q", inst.Action)
	}

	var svcs []string
	for _, svcName := range serviceNames {
		if svcName == snapName {
			// all services of the snap
			svcs = nil
			break
		}
		app, ok := info.Apps[svcName]
		if !(ok && app.IsService()) {
			return nil, fmt.Errorf(i18n.G("unknown service: %q"), svcName)
		}
		svcs = append(svcs, app.Name)
	}
	cmd.Services = svcs

	var summary string
	if len(svcs) > 0 {
		summary = fmt.Sprintf("Run service command %q for services %q of snap %q", cmd.Action, svcs, cmd.SnapName)
	} else {
		summary = fmt.Sprintf("Run service command %q for services of snap %q", cmd.Action, cmd.SnapName)
	}
	task := st.NewTask("service-control", summary)
	task.Set("service-action", cmd)
	task.Logf("args: %q, svcs: %q", serviceNames, svcs)
	return task, nil
}

// Control creates a taskset for starting/stopping/restarting services via systemctl.
// The appInfos and inst define the services and the command to execute.
// Context is used to determine change conflicts - we will not conflict with
// tasks from same change as that of context's.
func Control(st *state.State, appInfos []*snap.AppInfo, inst *Instruction, context *hookstate.Context) ([]*state.TaskSet, error) {
	var tts []*state.TaskSet

	var ctlcmds []string
	switch {
	case inst.Action == "start":
		if inst.Enable {
			ctlcmds = []string{"enable"}
		}
		ctlcmds = append(ctlcmds, "start")
	case inst.Action == "stop":
		if inst.Disable {
			ctlcmds = []string{"disable"}
		}
		ctlcmds = append(ctlcmds, "stop")
	case inst.Action == "restart":
		if inst.Reload {
			ctlcmds = []string{"reload-or-restart"}
		} else {
			ctlcmds = []string{"restart"}
		}
	default:
		return nil, fmt.Errorf("unknown action %q", inst.Action)
	}

	st.Lock()
	defer st.Unlock()

	svcs := make([]string, 0, len(appInfos))
	snapNames := make([]string, 0, len(appInfos))
	lastName := ""
	names := make([]string, len(appInfos))
	for i, svc := range appInfos {
		svcs = append(svcs, svc.ServiceName())
		snapName := svc.Snap.InstanceName()
		names[i] = snapName + "." + svc.Name
		if snapName != lastName {
			snapNames = append(snapNames, snapName)
			lastName = snapName
		}
	}

	ignoreChangeID := changeIDForContext(context)
	if err := snapstate.CheckChangeConflictMany(st, snapNames, ignoreChangeID); err != nil {
		return nil, &ServiceActionConflictError{err}
	}

	for _, cmd := range ctlcmds {
		argv := append([]string{"systemctl", cmd}, svcs...)
		desc := fmt.Sprintf("%s of %v", cmd, names)
		// Give the systemctl a maximum time of 61 for now.
		//
		// Longer term we need to refactor this code and
		// reuse the snapd/systemd and snapd/wrapper packages
		// to control the timeout in a single place.
		ts := cmdstate.ExecWithTimeout(st, desc, argv, 61*time.Second)

		// set ignore flag on the tasks, new snapd uses service-control tasks.
		ignore := true
		for _, t := range ts.Tasks() {
			t.Set("ignore", ignore)
		}
		tts = append(tts, ts)
	}

	// make a taskset wait for its predecessor
	for i := 1; i < len(tts); i++ {
		tts[i].WaitAll(tts[i-1])
	}

	return tts, nil
}
