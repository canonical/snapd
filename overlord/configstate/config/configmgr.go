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

// Package configstate implements the manager and state aspects responsible for
// the configuration of snaps.
package config

import (
	"regexp"

	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/hookstate/hook"
	"github.com/snapcore/snapd/overlord/state"
)

// ConfigManager is responsible for the maintenance of per-snap configuration in
// the system state.
type ConfigManager struct {
	state *state.State
}

// Manager returns a new ConfigManager.
func Manager(s *state.State, hookManager *hook.HookManager) (*ConfigManager, error) {
	manager := &ConfigManager{
		state: s,
	}

	hookManager.Register(regexp.MustCompile("^configure$"), configstate.NewConfigureHandler)

	return manager, nil
}
