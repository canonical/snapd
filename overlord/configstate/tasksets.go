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
	"fmt"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

// Change returns a taskset required to apply the given configuration
// patch.
func Change(s *state.State, snapName string, patchValues map[string]interface{}) *state.TaskSet {
	initialContext := map[string]interface{}{
		"patch": patchValues,
	}
	hookTaskSummary := fmt.Sprintf(i18n.G("Run apply-config hook for %s"), snapName)
	task := hookstate.HookTask(s, hookTaskSummary, snapName, snap.Revision{}, "apply-config", initialContext)
	return state.NewTaskSet(task)
}
