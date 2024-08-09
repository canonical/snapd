// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

/*
 * Copyright (C) 2024 Canonical Ltd
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

package configcore

import (
	"fmt"

	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/restart"
)

var restartRequest = restart.Request

// Trigger a security profile regeneration by restarting snapd if the
// experimental apparmor-prompting flag changed.
func doExperimentalApparmorPromptingDaemonRestart(c RunTransaction, opts *fsOnlyContext) error {
	st := c.State()

	snap, confName := features.AppArmorPrompting.ConfigOption()

	var prompting bool
	err := c.Get(snap, confName, &prompting)
	if err != nil && !config.IsNoOption(err) {
		return err
	}
	var prevPrompting bool
	err = c.GetPristine(snap, confName, &prevPrompting)
	if err != nil && !config.IsNoOption(err) {
		return err
	}
	if prompting == prevPrompting {
		return nil
	}

	if prompting {
		if is, whyNot := features.AppArmorPrompting.IsSupported(); !is {
			if whyNot == "" {
				// we don't have details as to why
				return fmt.Errorf("prompting feature is not supported by the system")
			}
			return fmt.Errorf("prompting feature is not supported by the system, reason: %s", whyNot)
		}
	}

	// No matter whether prompting is supported or not, request a restart of
	// snapd, since it may be the case that AppArmor has been updated and the
	// kernel or parser support for prompting has changed, and this isn't picked
	// up without re-probing the AppArmor features, which occurs during startup.

	st.Lock()
	defer st.Unlock()

	restartRequest(st, restart.RestartDaemon, nil)

	return nil
}
