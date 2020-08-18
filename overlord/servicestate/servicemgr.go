// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

// ServiceManager is responsible for starting and stopping snap services.
type ServiceManager struct {
	state *state.State
}

// Manager returns a new service manager.
func Manager(st *state.State, runner *state.TaskRunner) *ServiceManager {
	delayedCrossMgrInit()
	m := &ServiceManager{
		state: st,
	}
	// TODO: undo handler
	runner.AddHandler("service-control", m.doServiceControl, nil)
	return m
}

// Ensure implements StateManager.Ensure.
func (m *ServiceManager) Ensure() error {
	return nil
}

func delayedCrossMgrInit() {
	// hook into conflict checks mechanisms
	snapstate.AddAffectedSnapsByAttr("service-action", serviceControlAffectedSnaps)
}

func serviceControlAffectedSnaps(t *state.Task) ([]string, error) {
	var serviceAction ServiceAction
	if err := t.Get("service-action", &serviceAction); err != nil {
		return nil, fmt.Errorf("internal error: cannot obtain service action from task: %s", t.Summary())
	}
	return []string{serviceAction.SnapName}, nil
}
