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
	"time"

	"github.com/snapcore/snapd/i18n/dumb"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

func init() {
	snapstate.Configure = Configure
}

// Configure returns a taskset to apply the given configuration patch.
func Configure(s *state.State, snapName string, patch map[string]interface{}, flags int) *state.TaskSet {
	hooksup := &hookstate.HookSetup{
		Snap:       snapName,
		Hook:       "configure",
		Optional:   len(patch) == 0,
		IgnoreFail: flags&snapstate.IgnoreHookFailure > 0,
		// all configure hooks must finish within 1 minute
		MaxRuntime: 1 * time.Minute,
	}
	var contextData map[string]interface{}
	if len(patch) > 0 {
		contextData = map[string]interface{}{"patch": patch}
	}
	var summary string
	if hooksup.Optional {
		summary = fmt.Sprintf(i18n.G("Run configure hook of %q snap if present"), snapName)
	} else {
		summary = fmt.Sprintf(i18n.G("Run configure hook of %q snap"), snapName)
	}
	task := hookstate.HookTask(s, summary, hooksup, contextData)
	return state.NewTaskSet(task)
}
