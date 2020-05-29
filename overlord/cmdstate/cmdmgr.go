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

var defaultExecTimeout = 5 * time.Second

func doExec(t *state.Task, tomb *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	var ignore bool
	if err := t.Get("ignore", &ignore); err != nil && err != state.ErrNoState {
		return err
	}
	if ignore {
		t.Logf("task ignored")
		return nil
	}

	var argv []string
	var tout time.Duration
	if err := t.Get("argv", &argv); err != nil {
		return err
	}

	err := t.Get("timeout", &tout)
	// timeout is optional and might not be set
	if err != nil && err != state.ErrNoState {
		return err
	}
	if err == state.ErrNoState {
		tout = defaultExecTimeout
	}

	// the command needs to be run with unlocked state, but after that
	// we need to restore the lock (for Errorf, and for deferred unlocking
	// above).
	st.Unlock()
	buf, err := osutil.RunAndWait(argv, nil, tout, tomb)
	st.Lock()

	if err != nil {
		t.Errorf("# %s\n%s", strings.Join(argv, " "), buf)
		return err
	}

	return nil
}
