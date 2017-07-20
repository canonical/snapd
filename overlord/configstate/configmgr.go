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

	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/state"
)

// ConfigManager is responsible for the maintenance of per-snap configuration in
// the system state.
type ConfigManager struct {
	state *state.State
}

// Manager returns a new ConfigManager.
func Manager(s *state.State, hookManager *hookstate.HookManager) (*ConfigManager, error) {
	manager := &ConfigManager{
		state: s,
	}

	hookManager.Register(regexp.MustCompile("^configure$"), newConfigureHandler)

	return manager, nil
}
