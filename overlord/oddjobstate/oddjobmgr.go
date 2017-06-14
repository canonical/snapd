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
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/overlord/state"
)

// OddJobManager is responsible for running arbitrary commands as tasks.
type OddJobManager struct {
	runner *state.TaskRunner
}

// Manager returns a new OddJobManager.
func Manager(st *state.State) *OddJobManager {
	runner := state.NewTaskRunner(st)
	runner.AddHandler("exec", doExec, nil)
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

func doExec(t *state.Task, tomb *tomb.Tomb) error {
	var argv []string
	st := t.State()
	st.Lock()
	err := t.Get("argv", &argv)
	st.Unlock()
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = nil
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		<-tomb.Dying()
		cmd.Process.Kill()
	}()
	if err := cmd.Wait(); err != nil {
		st.Lock()
		if t.Status() == state.AbortStatus {
			t.Logf("aborted")
		} else {
			fmt.Fprintln(&buf, err)
			t.Errorf("task %q failed:\n# %s\n%s", t.Summary(), strings.Join(argv, " "), buf.String())
		}
		st.Unlock()
		return err
	}

	return nil
}
