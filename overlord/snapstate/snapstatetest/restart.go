// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2021 Canonical Ltd
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

package snapstatetest

import (
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/state"
)

// MockRestartHandler mocks a restart.Handler based on a function
// to witness the restart requests.
type MockRestartHandler func(restart.RestartType)

func (h MockRestartHandler) HandleRestart(t restart.RestartType) {
	if h == nil {
		return
	}
	h(t)
}

func (h MockRestartHandler) RebootAsExpected(*state.State) error {
	return nil
}

func (h MockRestartHandler) RebootDidNotHappen(*state.State) error {
	panic("internal error: mocking should not invoke RebootDidNotHappen")
}
