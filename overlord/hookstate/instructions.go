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

package hookstate

import (
	"fmt"

	"github.com/snapcore/snapd/hooks"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/state"
)

// RunHook returns a set of tasks for running a specific hook.
func RunHook(s *state.State, snapName, hookName string) (*state.TaskSet, error) {
	summary := fmt.Sprintf(i18n.G("%s: run %s hook"), snapName, hookName)
	task := s.NewTask("run-hook", summary)
	task.Set("hook", hooks.HookRef{Snap: snapName, Hook: hookName})
	return state.NewTaskSet(task), nil
}
