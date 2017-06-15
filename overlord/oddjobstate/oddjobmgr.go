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

package oddjobstate

import (
	"strings"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/state"
)

// OddJobManager helps running arbitrary commands as tasks.
type OddJobManager struct {
	runner *state.TaskRunner
}

// Manager returns a new OddJobManager.
func Manager(st *state.State) *OddJobManager {
	runner := state.NewTaskRunner(st)
	runner.AddHandler("exec-command", doExec, nil)
	return &OddJobManager{runner: runner}
}

// Ensure is part of the overlord.StateManager interface.
func (m *OddJobManager) Ensure() error {
	m.runner.Ensure()
	return nil
}

// Wait is part of the overlord.StateManager interface.
func (m *OddJobManager) Wait() {
	m.runner.Wait()
}

// Stop is part of the overlord.StateManager interface.
func (m *OddJobManager) Stop() {
	m.runner.Stop()
}

var execTimeout = 5 * time.Second

func doExec(t *state.Task, tomb *tomb.Tomb) error {
	var argv []string
	st := t.State()
	st.Lock()
	err := t.Get("argv", &argv)
	st.Unlock()
	if err != nil {
		return err
	}

	if buf, err := osutil.RunAndWait(argv, nil, execTimeout, tomb); err != nil {
		st.Lock()
		t.Errorf("# %s\n%s", strings.Join(argv, " "), buf)
		st.Unlock()
		return err
	}

	return nil
}
