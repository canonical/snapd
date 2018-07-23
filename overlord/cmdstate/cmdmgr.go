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

package cmdstate

import (
	"strings"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/state"
)

// CommandManager helps running arbitrary commands as tasks.
type CommandManager struct{}

// Manager returns a new CommandManager.
func Manager(st *state.State, runner *state.TaskRunner) *CommandManager {
	runner.AddHandler("exec-command", doExec, nil)
	return &CommandManager{}
}

// Ensure is part of the overlord.StateManager interface.
func (m *CommandManager) Ensure() error {
	return nil
}

// Wait is part of the overlord.StateManager interface.
func (m *CommandManager) Wait() {
}

// Stop is part of the overlord.StateManager interface.
func (m *CommandManager) Stop() {
}

var defaultExecTimeout = 5 * time.Second

func doExec(t *state.Task, tomb *tomb.Tomb) error {
	var argv []string
	var tout time.Duration

	st := t.State()
	st.Lock()
	err1 := t.Get("argv", &argv)
	err2 := t.Get("timeout", &tout)
	st.Unlock()
	if err1 != nil {
		return err1
	}
	if err2 != nil && err2 != state.ErrNoState {
		return err2
	}
	if err2 == state.ErrNoState {
		tout = defaultExecTimeout
	}

	if buf, err := osutil.RunAndWait(argv, nil, tout, tomb); err != nil {
		st.Lock()
		t.Errorf("# %s\n%s", strings.Join(argv, " "), buf)
		st.Unlock()
		return err
	}

	return nil
}
