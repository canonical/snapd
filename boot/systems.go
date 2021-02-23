// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package boot

import (
	"github.com/snapcore/snapd/asserts"
)

func observeSuccessfulSystems(model *asserts.Model, m *Modeenv) (*Modeenv, error) {
	// updates happen in run mode only
	if m.Mode != "run" {
		return m, nil
	}

	// compatibility scenario, no good systems are tracked in modeenv yet,
	// and there is a single entry in the current systems list
	if len(m.GoodRecoverySystems) == 0 && len(m.CurrentRecoverySystems) == 1 {
		newM, err := m.Copy()
		if err != nil {
			return nil, err
		}
		newM.GoodRecoverySystems = []string{m.CurrentRecoverySystems[0]}
		return newM, nil
	}
	return m, nil
}
