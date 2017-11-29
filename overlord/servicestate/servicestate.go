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
func Control(st *state.State, appInfos []*snap.AppInfo, inst *Instruction, context *hookstate.Context) (*state.TaskSet, error) {
	// the argv to call systemctl will need at most one entry per appInfo,
	// plus one for "systemctl", one for the action, and sometimes one for
	// an option. That's a maximum of 3+len(appInfos).
	argv := make([]string, 2, 3+len(appInfos))
	argv[0] = "systemctl"

	argv[1] = inst.Action
	switch inst.Action {
	case "start":
		if inst.Enable {
			argv[1] = "enable"
			argv = append(argv, "--now")
		}
	case "stop":
		if inst.Disable {
			argv[1] = "disable"
			argv = append(argv, "--now")
		}
	case "restart":
		if inst.Reload {
			argv[1] = "reload-or-restart"
		}
	default:
		return nil, fmt.Errorf("unknown action %q", inst.Action)
	}

	snapNames := make([]string, 0, len(appInfos))
	lastName := ""
	names := make([]string, len(appInfos))
	for i, svc := range appInfos {
		argv = append(argv, svc.ServiceName())
		snapName := svc.Snap.Name()
		names[i] = snapName + "." + svc.Name
		if snapName != lastName {
			snapNames = append(snapNames, snapName)
			lastName = snapName
		}
	}

	desc := fmt.Sprintf("%s of %v", inst.Action, names)

	st.Lock()
	defer st.Unlock()

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

	return cmdstate.Exec(st, desc, argv), nil
}
