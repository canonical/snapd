// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

// ConfigManager is responsible for the maintenance of per-snap
// configuration in the system state. It is not a "real" manager as it
// passes the heavy lifting on to the HookManager.
type ConfigManager struct{}

var corecfgRun = corecfg.Run

func MockCorecfgRun(f func(conf corecfg.Conf) error) (restore func()) {
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
		ctx.Lock()
		tr := ContextTransaction(ctx)
		ctx.Unlock()
		return corecfgRun(tr)
	})

	return &ConfigManager{}, nil
}

// Ensure implements StateManager.Ensure.
func (m *ConfigManager) Ensure() error {
	return nil
}

// Wait implements StateManager.Wait.
func (m *ConfigManager) Wait() {
}

// Stop implements StateManager.Stop.
func (m *ConfigManager) Stop() {
}
