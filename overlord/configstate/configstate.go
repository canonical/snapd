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
package configstate

import (
	"fmt"
	"os"
	"time"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

func init() {
	snapstate.Configure = Configure
}

func ConfigureHookTimeout() time.Duration {
	timeout := 5 * time.Minute
	if s := os.Getenv("SNAPD_CONFIGURE_HOOK_TIMEOUT"); s != "" {
		if to, err := time.ParseDuration(s); err == nil {
			timeout = to
		}
	}
	return timeout
}

// Configure returns a taskset to apply the given configuration patch.
func Configure(st *state.State, snapName string, patch map[string]interface{}, flags int) *state.TaskSet {
	hooksup := &hookstate.HookSetup{
		Snap:        snapName,
		Hook:        "configure",
		Optional:    len(patch) == 0,
		IgnoreError: flags&snapstate.IgnoreHookError != 0,
		TrackError:  flags&snapstate.TrackHookError != 0,
		// all configure hooks must finish within this timeout
		Timeout: ConfigureHookTimeout(),
	}
	var contextData map[string]interface{}
	if flags&snapstate.UseConfigDefaults != 0 {
		contextData = map[string]interface{}{"use-defaults": true}
	} else if len(patch) > 0 {
		contextData = map[string]interface{}{"patch": patch}
	}
	var summary string
	if hooksup.Optional {
		summary = fmt.Sprintf(i18n.G("Run configure hook of %q snap if present"), snapName)
	} else {
		summary = fmt.Sprintf(i18n.G("Run configure hook of %q snap"), snapName)
	}

	// configuration of "core" is handled differently
	if snapName == "core" {
		// TODO: respect IgnoreHookError and TrackHookError ?
		t := st.NewTask("configure-snapd", summary)
		t.Set("patch", patch)
		if v, ok := contextData["use-defaults"].(bool); v && ok {
			t.Set("use-defaults", true)
		}
		return state.NewTaskSet(t)
	}

	task := hookstate.HookTask(st, summary, hooksup, contextData)
	return state.NewTaskSet(task)
}
