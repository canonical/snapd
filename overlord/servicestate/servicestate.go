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

	var checkConflict func(otherTask *state.Task) bool
	if context != nil && !context.IsEphemeral() {
		if task, ok := context.Task(); ok {
			chg := task.Change()
			checkConflict = func(otherTask *state.Task) bool {
				if chg != nil && otherTask.Change() != nil {
					// if same change, then return false (no conflict)
					return chg.ID() != otherTask.Change().ID()
				}
				return true
			}
		}
	}

	if err := snapstate.CheckChangeConflictMany(st, snapNames, checkConflict); err != nil {
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
		tts = append(tts, ts)
	}

	// make a taskset wait for its predecessor
	for i := 1; i < len(tts); i++ {
		tts[i].WaitAll(tts[i-1])
	}

	return tts, nil
}
