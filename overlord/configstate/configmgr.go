// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package configstate

import (
	"regexp"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/corecfg"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

// ConfigManager is responsible for the maintenance of per-snap configuration in
// the system state.
type ConfigManager struct {
	state  *state.State
	runner *state.TaskRunner
}

// Manager returns a new ConfigManager.
func Manager(st *state.State, hookManager *hookstate.HookManager) (*ConfigManager, error) {
	// Most configuration is handled via the "configure" hook of the
	// snaps. However some configuration is internally handled
	hookManager.Register(regexp.MustCompile("^configure$"), newConfigureHandler)

	// we handle core/snapd specific configuration internally because
	// on classic systems we may need to configure things before any
	// snap is installed.
	runner := state.NewTaskRunner(st)
	manager := &ConfigManager{
		state:  st,
		runner: runner,
	}
	runner.AddHandler("configure-snapd", manager.doRunCoreConfigure, nil)

	return manager, nil
}

// Ensure implements StateManager.Ensure.
func (m *ConfigManager) Ensure() error {
	m.runner.Ensure()
	return nil
}

// Wait implements StateManager.Wait.
func (m *ConfigManager) Wait() {
	m.runner.Wait()
}

// Stop implements StateManager.Stop.
func (m *ConfigManager) Stop() {
	m.runner.Stop()
}

var corecfgRun = corecfg.Run

func MockCorecfgRun(f func(tr corecfg.Conf) error) (restore func()) {
	origCorecfgRun := corecfgRun
	corecfgRun = f
	return func() {
		corecfgRun = origCorecfgRun
	}
}

func (m *ConfigManager) doRunCoreConfigure(t *state.Task, tomb *tomb.Tomb) error {
	var patch map[string]interface{}
	var useDefaults bool

	st := t.State()
	st.Lock()
	defer st.Unlock()

	// FIXME: duplicated code from configureHandler.Before()
	if err := t.Get("use-defaults", &useDefaults); err != nil && err != state.ErrNoState {
		return err
	}
	if useDefaults {
		var err error
		patch, err = snapstate.ConfigDefaults(st, "core")
		if err != nil && err != state.ErrNoState {
			return err
		}
	} else {
		if err := t.Get("patch", &patch); err != nil && err != state.ErrNoState {
			return err
		}
	}

	tr := config.NewTransaction(st)
	st.Unlock()
	defer st.Lock()
	for key, value := range patch {
		if err := tr.Set("core", key, value); err != nil {
			return err
		}
	}

	if err := corecfgRun(tr); err != nil {
		return err
	}

	st.Lock()
	tr.Commit()
	st.Unlock()
	return nil
}
