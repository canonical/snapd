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

	"github.com/snapcore/snapd/corecfg"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/state"
)

// ConfigManager is responsible for the maintenance of per-snap configuration in
// the system state.
type ConfigManager struct {
	state  *state.State
	runner *state.TaskRunner
}

var corecfgRun = func(ctx *hookstate.Context) error {
	ctx.Lock()
	tr := ContextTransaction(ctx)
	ctx.Unlock()
	return corecfg.Run(tr)
}

func MockCorecfgRun(f func(ctx *hookstate.Context) error) (restore func()) {
	origCorecfgRun := corecfgRun
	corecfgRun = f
	return func() {
		corecfgRun = origCorecfgRun
	}
}

// Manager returns a new ConfigManager.
func Manager(st *state.State, hookManager *hookstate.HookManager) (*ConfigManager, error) {
	// Most configuration is handled via the "configure" hook of the
	// snaps. However some configuration is internally handled
	hookManager.Register(regexp.MustCompile("^configure$"), newConfigureHandler)
	// Ensure that we run configure for the core snap internally.
	// Note that we use the func() indirection so that mocking corecfgRun
	// in tests works correctly.
	hookManager.RegisterHijack("configure", "core", func(ctx *hookstate.Context) error {
		return corecfgRun(ctx)
	})

	// we handle core/snapd specific configuration internally because
	// on classic systems we may need to configure things before any
	// snap is installed.
	runner := state.NewTaskRunner(st)
	manager := &ConfigManager{
		state:  st,
		runner: runner,
	}

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
